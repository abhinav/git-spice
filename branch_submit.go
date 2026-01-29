package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/claude"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/submit"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

// errSummaryCancelled indicates the user cancelled PR summary generation.
var errSummaryCancelled = errors.New("summary cancelled")

// submitOptions defines options that are common to all submit commands.
type submitOptions struct {
	submit.Options

	NoWeb         bool `help:"Alias for --web=false."`
	ClaudeSummary bool `help:"Generate PR title and body using Claude AI."`

	// TODO: Other creation options e.g.:
	// - milestone
}

const _submitHelp = `
Use --dry-run to print what would be submitted without submitting it.

For new Change Requests, a prompt will allow filling metadata.
Use --fill to populate title and body from the commit messages.
The --[no-]draft flag marks the CR as draft or not.
Use the 'spice.submit.draft' configuration option
to mark new CRs as drafts (or not) by default,
skipping the prompt.

For updating Change Requests,
use --[no-]draft to change its draft status.
Without the flag, the draft status is not changed.

Use --no-publish to push branches without creating CRs.
This has no effect if a branch already has an open CR.

Use --update-only to only update branches with existing CRs,
and skip those that would create new CRs.

Use --nav-comment=false to disable navigation comments in CRs,
or --nav-comment=multiple to post those comments
only if there are multiple CRs in the stack.
`

type branchSubmitCmd struct {
	submitOptions

	Title  string `help:"Title of the change request" placeholder:"TITLE"`
	Body   string `help:"Body of the change request" placeholder:"BODY"`
	Branch string `placeholder:"NAME" help:"Branch to submit" predictor:"trackedBranches"`
	Base   string `short:"b" placeholder:"BRANCH" help:"Base branch for the PR (overrides tracked base)"`
}

func (*branchSubmitCmd) Help() string {
	return text.Dedent(`
		A Change Request is created for the current branch,
		or updated if it already exists.
		Use the --branch flag to target a different branch.

		Use --base to specify a different base branch for the PR.
		This overrides the tracked base branch.

		For new Change Requests, a prompt will allow filling metadata.
		Use the --title and --body flags to skip the prompt,
		or the --fill flag to use the commit message to fill them in.
		The --[no-]draft flag marks the CR as draft or not.
		Use the 'spice.submit.draft' configuration option
		to mark new CRs as drafts (or not) by default,
		skipping the prompt.

		For updating Change Requests,
		use --[no-]draft to change its draft status.
		Without the flag, the draft status is not changed.

		Use --no-publish to push branches without creating CRs.
		This has no effect if a branch already has an open CR.

		Use --update-only to only update branches with existing CRs,
		and skip those that would create new CRs.

		Use --nav-comment=false to disable navigation comments in CRs,
		or --nav-comment=multiple to post those comments
		only if there are multiple CRs in the stack.
	`)
}

// SubmitHandler submits change requests to a forge.
type SubmitHandler interface {
	Submit(ctx context.Context, req *submit.Request) error
	SubmitBatch(ctx context.Context, req *submit.BatchRequest) error
}

func (cmd *branchSubmitCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	wt *git.Worktree,
	store *state.Store,
	submitHandler SubmitHandler,
) error {
	if cmd.NoWeb {
		cmd.Web = submit.OpenWebNever
	}

	if cmd.Branch == "" {
		currentBranch, err := wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = currentBranch
	}

	title := cmd.Title
	body := cmd.Body

	// Generate PR summary with Claude if requested.
	if cmd.ClaudeSummary {
		switch {
		case title != "":
			log.Warn("--claude-summary ignored because --title was provided")
		default:
			if body != "" {
				log.Warn("--body will be overwritten by --claude-summary")
			}
			var err error
			title, body, err = generatePRSummary(ctx, log, view, repo, store, cmd.Branch, cmd.Base)
			if err != nil {
				if errors.Is(err, errSummaryCancelled) {
					return err
				}
				return fmt.Errorf("generate PR summary: %w", err)
			}
			// Empty title with no error means no diff available;
			// continue with submit (will prompt for metadata).
		}
	}

	return submitHandler.Submit(ctx, &submit.Request{
		Branch:  cmd.Branch,
		Title:   title,
		Body:    body,
		Base:    cmd.Base,
		Options: &cmd.Options,
	})
}

// generatePRSummary generates a PR title and body using Claude.
// If baseOverride is non-empty, it is used instead of the tracked base.
func generatePRSummary(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	store *state.Store,
	branch string,
	baseOverride string,
) (title, body string, err error) {
	// Determine base branch: use override if provided,
	// otherwise use tracked state, fallback to trunk for untracked branches.
	var base string
	if baseOverride != "" {
		base = baseOverride
	} else {
		base = store.Trunk()
		if base == "" {
			return "", "", errors.New("could not determine trunk branch")
		}
		branchInfo, err := store.LookupBranch(ctx, branch)
		if err != nil {
			// Only fall back to trunk if branch is not tracked.
			// Other errors (storage issues) should be reported.
			if !errors.Is(err, state.ErrNotExist) {
				return "", "", fmt.Errorf("look up branch %s: %w", branch, err)
			}
			log.Info("Branch not tracked, using trunk as base", "branch", branch)
		} else if branchInfo.Base != "" {
			base = branchInfo.Base
		}
	}

	// Get diff between base and branch.
	diffText, err := repo.DiffText(ctx, base, branch)
	if err != nil {
		return "", "", fmt.Errorf("get diff %s...%s: %w", base, branch, err)
	}

	if diffText == "" {
		// No code changes, but allow the submit to continue
		// (PR metadata can still be updated).
		log.Warn("No changes to summarize; " +
			"--claude-summary requires code changes to generate a summary")
		return "", "", nil
	}

	// Prepare diff for Claude.
	prepared, err := claude.PrepareDiff(diffText, &claude.PrepareDiffOptions{
		Log: log,
	})
	if err != nil {
		return "", "", err
	}

	// Get commit messages for context.
	commits, err := repo.CommitMessageRange(ctx, branch, base)
	if err != nil {
		log.Warn("Could not get commit messages", "error", err)
	}

	var commitSummary strings.Builder
	for _, c := range commits {
		commitSummary.WriteString("- ")
		commitSummary.WriteString(c.Subject)
		commitSummary.WriteString("\n")
	}

	// Build prompt and run.
	prompt := claude.BuildSummaryPrompt(
		prepared.Config, branch, base, commitSummary.String(), prepared.FilteredDiff,
	)

	fmt.Fprint(view, "Generating PR summary with Claude... ")
	response, err := prepared.Client.SendPromptWithModel(
		ctx, prompt, prepared.Config.Models.Summary,
	)
	fmt.Fprintln(view, "done")
	if err != nil {
		return "", "", claude.RunClaudeError(err)
	}

	// Parse the response to extract title and body.
	title, body = claude.ParseTitleBody(response)

	// Show preview and get user choice.
	return showSummaryPreview(view, title, body)
}

// showSummaryPreview shows the generated PR summary and lets user accept/edit.
func showSummaryPreview(view ui.View, title, body string) (string, string, error) {
	// For non-interactive mode, just return.
	if !ui.Interactive(view) {
		return title, body, nil
	}

	// Show preview.
	fmt.Fprintln(view, "")
	fmt.Fprintln(view, "=== Claude suggests ===")
	fmt.Fprintln(view, "Title:", title)
	if body != "" {
		fmt.Fprintln(view, "")
		fmt.Fprintln(view, body)
	}
	fmt.Fprintln(view, "=======================")
	fmt.Fprintln(view, "")

	// Ask for confirmation.
	type choice int
	const (
		choiceAccept choice = iota
		choiceCancel
	)

	var selected choice
	field := ui.NewSelect[choice]().
		WithTitle("Action").
		WithValue(&selected).
		WithOptions(
			ui.SelectOption[choice]{Label: "Accept", Value: choiceAccept},
			ui.SelectOption[choice]{Label: "Cancel", Value: choiceCancel},
		)

	if err := ui.Run(view, field); err != nil {
		return "", "", err
	}

	switch selected {
	case choiceAccept:
		return title, body, nil
	default: // choiceCancel
		return "", "", errSummaryCancelled
	}
}
