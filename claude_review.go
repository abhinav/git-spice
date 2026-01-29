package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"go.abhg.dev/gs/internal/claude"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type claudeReviewCmd struct {
	From      string `help:"Start of the range to review (defaults to trunk)"`
	To        string `help:"End of the range to review (defaults to current branch)"`
	PerBranch bool   `help:"Review each branch individually, then provide an overall summary"`
	Title     string `help:"Title for the review (defaults to branch name or range)"`
	Fix       bool   `help:"After review, prompt to apply suggested fixes"`
}

func (*claudeReviewCmd) Help() string {
	return text.Dedent(`
		Reviews code changes using Claude AI.

		By default, reviews all changes from the trunk to the current branch.
		Use --from and --to flags to specify a custom range.

		The --per-branch flag reviews each branch in the stack individually,
		then provides an overall summary of the entire stack.

		Example usage:
		  gs claude review                      # Review current branch against trunk
		  gs claude review --from main --to feature
		  gs claude review --per-branch         # Review each branch in stack
	`)
}

func (cmd *claudeReviewCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
) error {
	// Initialize Claude client.
	client := claude.NewClient(nil)
	if !client.IsAvailable() {
		return errors.New("claude CLI not found; please install it from https://claude.ai/download")
	}

	// Load configuration.
	cfg, err := claude.LoadConfig(claude.DefaultConfigPath())
	if err != nil {
		log.Warn("Could not load claude config, using defaults", "error", err)
		cfg = claude.DefaultConfig()
	}

	// Determine the range.
	fromRef := cmd.From
	if fromRef == "" {
		fromRef = store.Trunk()
	}

	toRef := cmd.To
	if toRef == "" {
		currentBranch, err := wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		toRef = currentBranch
	}

	title := cmd.Title
	if title == "" {
		if fromRef == store.Trunk() {
			title = toRef
		} else {
			title = fromRef + "..." + toRef
		}
	}

	if cmd.PerBranch {
		return cmd.runPerBranch(ctx, log, view, repo, svc, store, client, cfg, fromRef, toRef, title)
	}

	return cmd.runOverall(ctx, log, view, repo, client, cfg, fromRef, toRef, title)
}

func (cmd *claudeReviewCmd) runOverall(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	client *claude.Client,
	cfg *claude.Config,
	fromRef, toRef, title string,
) error {
	log.Infof("Reviewing changes: %s...%s", fromRef, toRef)

	diffText, err := repo.DiffText(ctx, fromRef, toRef)
	if err != nil {
		return fmt.Errorf("get diff: %w", err)
	}
	if diffText == "" {
		log.Info("No changes to review")
		return nil
	}

	result, err := claude.ParseAndFilterDiff(diffText, cfg)
	if err != nil {
		return fmt.Errorf("parse diff: %w", err)
	}
	if len(result.Files) == 0 {
		log.Info("No changes to review after filtering")
		return nil
	}
	if result.Budget.OverBudget {
		return cmd.handleOverBudget(view, result.Budget)
	}

	prompt := claude.BuildReviewPrompt(cfg, title, result.FilteredDiff)

	fmt.Fprint(view, "Sending to Claude for review... ")
	response, err := client.SendPromptWithModel(ctx, prompt, cfg.Models.Review)
	fmt.Fprintln(view, "done")
	if err != nil {
		return cmd.handleClaudeError(err)
	}

	fmt.Fprintln(view, "")
	fmt.Fprintln(view, "=== Claude Review ===")
	fmt.Fprintln(view, "")
	fmt.Fprintln(view, response)

	if cmd.Fix && ui.Interactive(view) {
		return cmd.offerFixes(ctx, view, client, cfg, response, result.FilteredDiff)
	}

	return nil
}

func (cmd *claudeReviewCmd) runPerBranch(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	svc *spice.Service,
	store *state.Store,
	client *claude.Client,
	cfg *claude.Config,
	fromRef, toRef, title string,
) error {
	graph, err := svc.BranchGraph(ctx, nil)
	if err != nil {
		return fmt.Errorf("load branch graph: %w", err)
	}

	pathResult := collectBranchPath(graph, store.Trunk(), toRef)
	if len(pathResult.Branches) == 0 {
		log.Info("No tracked branches found in range")
		return cmd.runOverall(ctx, log, view, repo, client, cfg, fromRef, toRef, title)
	}
	if pathResult.Incomplete {
		log.Warn("Branch path incomplete; branch not found in graph",
			"branch", pathResult.MissingBranch)
	}

	// Review each branch individually.
	// Note: Each branch's diff is unique (base...branch shows only that branch's
	// changes), so there's no redundancy even for stacks.
	var reviews []string
	for _, branch := range pathResult.Branches {
		result, err := cmd.reviewSingleBranch(
			ctx, log, view, repo, graph, store, client, cfg, branch,
		)
		if err != nil {
			return err
		}
		if result.Reviewed {
			reviews = append(reviews, result.Content)
		}
	}

	// Generate stack summary if multiple branches were reviewed.
	if len(reviews) > 1 {
		if err := cmd.generateStackSummary(ctx, view, client, cfg, reviews); err != nil {
			return err
		}
	}

	return nil
}

// branchReviewResult holds the result of reviewing a single branch.
type branchReviewResult struct {
	// Content is the review text formatted for inclusion in stack summary.
	Content string
	// Reviewed is true if the branch was actually reviewed.
	// False if skipped due to no changes.
	Reviewed bool
}

// reviewSingleBranch reviews a single branch.
// Returns Reviewed=false if the branch has no reviewable changes.
// Returns an error if the branch exceeds the budget or review fails.
func (cmd *claudeReviewCmd) reviewSingleBranch(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	graph *spice.BranchGraph,
	store *state.Store,
	client *claude.Client,
	cfg *claude.Config,
	branch string,
) (branchReviewResult, error) {
	info, ok := graph.Lookup(branch)
	if !ok {
		log.Warn("Branch not found in graph, skipping", "branch", branch)
		return branchReviewResult{Reviewed: false}, nil
	}

	base := info.Base
	if base == "" {
		base = store.Trunk()
	}
	log.Infof("Reviewing branch: %s (base: %s)", branch, base)

	diffText, err := repo.DiffText(ctx, base, branch)
	if err != nil {
		return branchReviewResult{}, fmt.Errorf("get diff for %s: %w", branch, err)
	}
	if diffText == "" {
		log.Infof("Branch %s has no changes", branch)
		return branchReviewResult{Reviewed: false}, nil
	}

	result, err := claude.ParseAndFilterDiff(diffText, cfg)
	if err != nil {
		return branchReviewResult{}, fmt.Errorf("parse diff for %s: %w", branch, err)
	}
	if len(result.Files) == 0 {
		log.Infof("Branch %s has no changes after filtering", branch)
		return branchReviewResult{Reviewed: false}, nil
	}
	if result.Budget.OverBudget {
		return branchReviewResult{}, fmt.Errorf(
			"branch %s exceeds budget (%d lines > %d max)",
			branch, result.Budget.TotalLines, result.Budget.MaxLines,
		)
	}

	prompt := claude.BuildReviewPrompt(cfg, branch, result.FilteredDiff)

	fmt.Fprint(view, "Reviewing... ")
	response, err := client.SendPromptWithModel(ctx, prompt, cfg.Models.Review)
	fmt.Fprintln(view, "done")
	if err != nil {
		return branchReviewResult{}, cmd.handleClaudeError(err)
	}

	fmt.Fprintln(view, "")
	fmt.Fprintf(view, "=== Review: %s ===\n", branch)
	fmt.Fprintln(view, "")
	fmt.Fprintln(view, response)

	return branchReviewResult{
		Content:  fmt.Sprintf("## Branch: %s\n\n%s", branch, response),
		Reviewed: true,
	}, nil
}

// generateStackSummary generates and displays a summary of all branch reviews.
func (cmd *claudeReviewCmd) generateStackSummary(
	ctx context.Context,
	view ui.View,
	client *claude.Client,
	cfg *claude.Config,
	reviews []string,
) error {
	fmt.Fprint(view, "Generating stack summary... ")

	// Build stack summary with separator.
	var summary strings.Builder
	for i, review := range reviews {
		if i > 0 {
			summary.WriteString("\n\n---\n\n")
		}
		summary.WriteString(review)
	}

	prompt := claude.BuildStackReviewPrompt(cfg, summary.String())
	response, err := client.SendPromptWithModel(ctx, prompt, cfg.Models.Review)
	fmt.Fprintln(view, "done")
	if err != nil {
		return cmd.handleClaudeError(err)
	}

	fmt.Fprintln(view, "")
	fmt.Fprintln(view, "=== Stack Summary ===")
	fmt.Fprintln(view, "")
	fmt.Fprintln(view, response)

	return nil
}

func (cmd *claudeReviewCmd) handleOverBudget(view ui.View, budget claude.BudgetResult) error {
	fmt.Fprintf(view, "Diff too large (%d lines, budget: %d)\n", budget.TotalLines, budget.MaxLines)
	fmt.Fprintln(view, "")
	fmt.Fprintln(view, "Options:")
	fmt.Fprintln(view, "  1. Narrow range with --from/--to")
	fmt.Fprintln(view, "  2. Large files (add to ignorePatterns):")

	// Sort files by line count (descending).
	type fileEntry struct {
		path  string
		lines int
	}
	var entries []fileEntry
	for path, lines := range budget.FileLines {
		entries = append(entries, fileEntry{path, lines})
	}
	slices.SortFunc(entries, func(a, b fileEntry) int {
		return cmp.Compare(b.lines, a.lines) // descending
	})

	// Show top N largest files with suggested ignore patterns.
	const maxFilesToShow = 5
	for i := range min(len(entries), maxFilesToShow) {
		e := entries[i]
		ext := filepath.Ext(e.path)
		if ext != "" {
			fmt.Fprintf(view, "     - %s (%d lines) → add '*%s'\n", e.path, e.lines, ext)
		} else {
			fmt.Fprintf(view, "     - %s (%d lines)\n", e.path, e.lines)
		}
	}

	fmt.Fprintln(view, "")
	fmt.Fprintln(view, "Config file:", claude.DefaultConfigPath())

	return errors.New("diff exceeds budget")
}

func (cmd *claudeReviewCmd) handleClaudeError(err error) error {
	switch {
	case errors.Is(err, claude.ErrNotAuthenticated):
		return errors.New("not authenticated with Claude; please run 'claude auth'")
	case errors.Is(err, claude.ErrRateLimited):
		return errors.New("claude rate limit exceeded; please try again later")
	default:
		return fmt.Errorf("claude: %w", err)
	}
}

func (cmd *claudeReviewCmd) offerFixes(
	ctx context.Context,
	view ui.View,
	client *claude.Client,
	cfg *claude.Config,
	review, diff string,
) error {
	fmt.Fprintln(view, "")

	type choice int
	const (
		choiceApply choice = iota
		choiceSkip
	)

	var selected choice
	field := ui.NewSelect[choice]().
		WithTitle("Apply fixes?").
		WithValue(&selected).
		WithOptions(
			ui.SelectOption[choice]{Label: "Apply suggested fixes", Value: choiceApply},
			ui.SelectOption[choice]{Label: "Skip", Value: choiceSkip},
		)

	if err := ui.Run(view, field); err != nil {
		return err
	}

	if selected == choiceSkip {
		return nil
	}

	// Build fix prompt with review context.
	fixPrompt := `Based on the following code review, apply the suggested fixes.
Only modify files that need changes based on the review.
Do not add any new functionality beyond what the review suggests.

## Review:
` + review + `

## Current diff:
` + diff

	fmt.Fprint(view, "Applying fixes with Claude... ")
	response, err := client.SendPromptWithModel(ctx, fixPrompt, cfg.Models.Review)
	fmt.Fprintln(view, "done")
	if err != nil {
		return cmd.handleClaudeError(err)
	}

	fmt.Fprintln(view, "")
	fmt.Fprintln(view, "=== Applied Fixes ===")
	fmt.Fprintln(view, "")
	fmt.Fprintln(view, response)

	return nil
}

// branchPathResult holds the result of collecting a branch path.
type branchPathResult struct {
	// Branches is the list of branches from trunk-adjacent to target.
	Branches []string
	// Incomplete is true if the path couldn't reach trunk (missing branch in graph).
	Incomplete bool
	// MissingBranch is the branch that wasn't found (only set if Incomplete).
	MissingBranch string
}

// collectBranchPath collects branches from trunk to target in the branch graph.
// Returns branches in stack order from bottom to top:
// the branch closest to trunk is first, and the target branch is last.
// The trunk branch itself is not included in the result.
func collectBranchPath(graph *spice.BranchGraph, trunk, target string) branchPathResult {
	var result branchPathResult

	current := target
	for current != "" && current != trunk {
		result.Branches = append(result.Branches, current)

		info, ok := graph.Lookup(current)
		if !ok {
			result.Incomplete = true
			result.MissingBranch = current
			break
		}
		current = info.Base
	}

	// Reverse to get trunk-adjacent first, target last.
	slices.Reverse(result.Branches)

	return result
}
