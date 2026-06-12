// Package restack implements business logic for high-level restack operations.
package restack

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"iter"
	"slices"

	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/iterutil"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/scriptrun"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/spicedir"
	"go.abhg.dev/gs/internal/spice/state"
)

//go:generate mockgen -package restack -destination mocks_test.go . GitWorktree,Service

// GitWorktree is a subet of the git.Worktree interface.
type GitWorktree interface {
	CurrentBranch(ctx context.Context) (string, error)
	CheckoutBranch(ctx context.Context, branch string) error
	RootDir() string

	// RebaseContinue continues the currently in-progress rebase.
	// Used by the auto-resolve loop after the resolver script has
	// rewritten the conflicted files.
	RebaseContinue(ctx context.Context, opts *git.RebaseContinueOptions) error

	// ListFilesPaths enumerates files under the given selection.
	// Used to gather unmerged paths to pass to the resolver and to
	// stage afterwards.
	ListFilesPaths(ctx context.Context, opts *git.ListFilesOptions) iter.Seq2[string, error]

	// StageFiles stages the given paths via `git add`.
	StageFiles(ctx context.Context, paths []string) error
}

var _ GitWorktree = (*git.Worktree)(nil)

// Store is a subset of the state.Store interface.
type Store interface {
	Trunk() string
}

// Service is a subset of the spice.Service interface.
type Service interface {
	BranchGraph(ctx context.Context, opts *spice.BranchGraphOptions) (*spice.BranchGraph, error)
	Restack(ctx context.Context, name string) (*spice.RestackResponse, error)
	RebaseRescue(ctx context.Context, req spice.RebaseRescueRequest) error
	LookupBranch(ctx context.Context, name string) (*spice.LookupBranchResponse, error)
}

// Handler implements various restack operations.
type Handler struct {
	Log      *silog.Logger // required
	Worktree GitWorktree   // required
	Store    Store         // required
	Service  Service       // required

	// Resolver is invoked when a rebase conflicts and auto-resolve
	// is enabled. nil means no resolver is configured; conflicts
	// surface normally.
	Resolver Resolver

	// Prompter collects user answers when the resolver returns
	// questions. nil disables the question-iteration loop (questions
	// become an immediate error).
	Prompter QuestionPrompter

	// DefaultAutoResolve sets the default behavior when
	// [Request.AutoResolve] is nil. Typically populated from
	// spice.restack.autoResolve.
	DefaultAutoResolve bool

	// RepoRoot is the directory containing the resolution file.
	// Required for the auto-resolve loop.
	RepoRoot string

	// MaxResolveIterations bounds how many times the resolver may be
	// invoked for a single conflict. Typically populated from
	// spice.scriptResolve.maxIterations via
	// Config.ScriptResolveMaxIterations. A non-positive value falls
	// back to the package default.
	MaxResolveIterations int
}

// defaultMaxResolveIterations is the fallback used when Handler.MaxResolveIterations
// is non-positive. Matches DefaultScriptResolveMaxIterations in
// internal/spice.
const defaultMaxResolveIterations = 10

// resolveIterationCap returns the effective per-conflict iteration cap.
func (h *Handler) resolveIterationCap() int {
	if h.MaxResolveIterations > 0 {
		return h.MaxResolveIterations
	}
	return defaultMaxResolveIterations
}

// operationFromContinueCommand maps a Request.ContinueCommand back to
// the canonical operation name passed to the resolver. The
// ContinueCommand is always the (subcommand, action) pair used to
// resume an interrupted operation -- e.g. {"branch","restack"} ->
// "branch-restack". See doc/src/guide/scripts.md for the full table.
func operationFromContinueCommand(cmd []string) scriptrun.Operation {
	if len(cmd) == 0 {
		return ""
	}
	op := cmd[0]
	for _, part := range cmd[1:] {
		op = op + "-" + part
	}
	return scriptrun.Operation(op)
}

// Scope specifies which branches are affected
// by a restack operation.
type Scope int

const (
	// ScopeBranch selects just the branch specified in the request.
	ScopeBranch Scope = 1 << iota

	// ScopeUpstackExclusive selects the upstack of a branch,
	// excluding the branch itself.
	ScopeUpstackExclusive

	scopeDownstackExclusive

	// ScopeUpstack selects the upstack of a branch,
	// including the branch itself.
	ScopeUpstack = ScopeBranch | ScopeUpstackExclusive

	// ScopeDownstack selects the downstack of a branch,
	// including the branch itself.
	ScopeDownstack = ScopeBranch | scopeDownstackExclusive

	// ScopeStack selects the full stack of a branch:
	// the upstack, downstack, and the branch itself.
	ScopeStack = ScopeUpstack | ScopeDownstack
)

// Options carries per-invocation overrides that apply to every
// restack helper (Branch, Stack, Upstack, Downstack). They turn
// into command-line flags on the restack commands, so additions
// should consider end-user UX.
type Options struct {
	// AutoResolve, if non-nil, overrides
	// [Handler.DefaultAutoResolve] for this invocation. A true
	// value enables the configured resolver; a false value disables
	// it even when configured.
	AutoResolve *bool
}

// autoResolvePtr returns the AutoResolve pointer if opts is
// non-nil, or nil. Callers pass the result through to
// [Request.AutoResolve].
func (o *Options) autoResolvePtr() *bool {
	if o == nil {
		return nil
	}
	return o.AutoResolve
}

// Request is a request to restack one or more branches.
type Request struct {
	// Branch is the starting point for the restack operation.
	// This branch will be checked out at the end of the operation.
	//
	// Scope is relative to this branch.
	Branch string // required

	// ContinueCommand specifies the git-spice command
	// to run from the Branch's context
	// to resume this operation if it is interrupted
	// due to a conflict.
	ContinueCommand []string // required

	// Scope specifies which branches are affected by the restack operation.
	//
	// Defaults to ScopeBranch.
	Scope Scope

	// WorktreeFilter, if non-empty,
	// limits restacking to branches belonging to stacks
	// that have at least one branch
	// checked out in the given worktree.
	WorktreeFilter string

	// SkipCheckout skips checking out req.Branch
	// after restacking completes.
	// Use this when the caller handles checkout itself.
	SkipCheckout bool

	// AutoResolve, if non-nil, overrides
	// [Handler.DefaultAutoResolve] for this invocation. A true
	// value enables the configured resolver; a false value disables
	// it even when configured.
	AutoResolve *bool
}

// shouldAutoResolve resolves Request.AutoResolve against
// Handler.DefaultAutoResolve.
func (h *Handler) shouldAutoResolve(req *Request) bool {
	if req != nil && req.AutoResolve != nil {
		return *req.AutoResolve
	}
	return h.DefaultAutoResolve
}

// Restack restacks one or more branches according to the request.
func (h *Handler) Restack(ctx context.Context, req *Request) (int, error) {
	must.NotBeBlankf(req.Branch, "branch must not be blank")
	must.NotBeEmptyf(req.ContinueCommand, "continue command must not be set")

	req.Scope = cmp.Or(req.Scope, ScopeBranch) // 0 = ScopeBranch

	branchGraph, err := h.Service.BranchGraph(ctx, &spice.BranchGraphOptions{
		IncludeWorktrees: true,
	})
	if err != nil {
		return 0, fmt.Errorf("load branch graph: %w", err)
	}

	var branchesToRestack []string // branches in restack order

	if req.Scope&scopeDownstackExclusive != 0 {
		// Downstack returns the branches in the order,
		// [branch, downstack1, downstack2, ...],
		// not including the trunk.
		//
		// Restacking order is the reverse of that.
		downstack := slices.Collect(branchGraph.Downstack(req.Branch))
		if len(downstack) > 0 && downstack[0] == req.Branch {
			downstack = downstack[1:]
		}
		slices.Reverse(downstack)
		branchesToRestack = append(branchesToRestack, downstack...)
	}

	if req.Scope&ScopeBranch != 0 {
		if req.Branch == h.Store.Trunk() {
			// If we're explicitly only trying to restack trunk,
			// fail the operation.
			if req.Scope == ScopeBranch {
				return 0, errors.New("trunk cannot be restacked")
			}
		} else {
			branchesToRestack = append(branchesToRestack, req.Branch)
		}
	}

	if req.Scope&ScopeUpstackExclusive != 0 {
		// Upstacks returns the branches in the order,
		// [branch, upstack1, upstack2, ...].
		// That's restacking order, so we can use it directly
		// once we drop the first item (the branch itself).
		for idx, branch := range iterutil.Enumerate(branchGraph.Upstack(req.Branch)) {
			if idx == 0 && branch == req.Branch {
				continue // skip the branch itself
			}

			branchesToRestack = append(branchesToRestack, branch)
		}
	}

	// If a worktree filter is active,
	// keep only branches belonging to stacks
	// with at least one branch in the target worktree.
	if req.WorktreeFilter != "" {
		allowed := make(map[string]struct{})
		for stack := range branchGraph.StacksInWorktree(
			req.WorktreeFilter,
		) {
			for _, b := range stack {
				allowed[b] = struct{}{}
			}
		}

		filtered := branchesToRestack[:0]
		for _, branch := range branchesToRestack {
			if _, ok := allowed[branch]; ok {
				filtered = append(filtered, branch)
			}
		}
		branchesToRestack = filtered
	}

	// If any of the branches to be restacked
	// are checked out in another Git worktree,
	// we cannot restack anything upstack from that branch.
	//
	// And since branchesToRestack is in the restack order,
	// we can check if a prior skipped branch affects the current branch
	// by just checking the base of the skipped branch.
	currentWT := h.Worktree.RootDir()
	skipped := make(map[string]struct{})
	branchesToActuallyRestack := branchesToRestack[:0]
	var requestBranchWT string // worktree of request.Branch
	for _, branch := range branchesToRestack {
		if branch == h.Store.Trunk() {
			continue // skip restacking trunk branch
		}

		if info, ok := branchGraph.Lookup(branch); ok {
			if _, baseSkipped := skipped[info.Base]; baseSkipped {
				// Base branch not being restacked,
				// so skip this as well.
				h.Log.Warnf("%v: base branch %v was not restacked, skipping", branch, info.Base)
				skipped[branch] = struct{}{}
				continue
			}
		}

		branchWT := branchGraph.Worktree(branch)
		if req.Branch == branch {
			requestBranchWT = branchWT
		}
		if branchWT != "" && branchWT != currentWT {
			// Checked out in another worktree.
			h.Log.Warnf("%v: checked out in another worktree (%v), skipping", branch, branchWT)
			skipped[branch] = struct{}{}
			continue
		}

		branchesToActuallyRestack = append(branchesToActuallyRestack, branch)
	}
	branchesToRestack = branchesToActuallyRestack

	var restackCount int
loop:
	for _, branch := range branchesToRestack {
		res, err := h.Service.Restack(ctx, branch)
		if err != nil {
			var rebaseErr *git.RebaseInterruptError
			switch {
			case errors.As(err, &rebaseErr):
				if h.shouldAutoResolve(req) && h.Resolver != nil &&
					rebaseErr.Kind == git.RebaseInterruptConflict {
					resolved, baseName := h.tryAutoResolveRebase(ctx, operationFromContinueCommand(req.ContinueCommand), branch, branchGraph)
					if resolved {
						h.Log.Infof("%v: restacked on %v", branch, baseName)
						restackCount++
						continue loop
					}
					// Fall through to RebaseRescue on auto-resolve
					// failure: the rebase is still mid-flight in
					// the worktree, the user resolves manually
					// and runs 'gs rebase continue'.
				}

				// If the rebase is interrupted by a conflict,
				// we'll resume by re-running this command.
				return 0, h.Service.RebaseRescue(ctx, spice.RebaseRescueRequest{
					Err:     rebaseErr,
					Command: req.ContinueCommand,
					Branch:  req.Branch,
					Message: fmt.Sprintf("interrupted: restack branch %q", branch),
				})

			case errors.Is(err, state.ErrNotExist):
				h.Log.Errorf("%v: branch not tracked: run '%s branch track %v' to track it", branch, cli.Name(), branch)
				return 0, errors.New("untracked branch")

			case errors.Is(err, spice.ErrAlreadyRestacked):
				h.Log.Infof("%v: branch does not need to be restacked.", branch)
				continue loop

			default:
				return 0, fmt.Errorf("restack branch %q: %w", branch, err)
			}
		}

		h.Log.Infof("%v: restacked on %v", branch, res.Base)
		restackCount++
	}

	if requestBranchWT != "" && requestBranchWT != currentWT {
		h.Log.Warnf("%v: checked out in another worktree (%v), not checking out here", req.Branch, requestBranchWT)
	} else if restackCount > 0 && !req.SkipCheckout {
		if err := h.Worktree.CheckoutBranch(ctx, req.Branch); err != nil {
			return 0, fmt.Errorf("checkout branch %v: %w", req.Branch, err)
		}
	}

	return restackCount, nil
}

// tryAutoResolveRebase drives the resolver loop for an in-progress
// rebase of branch onto its base. Returns true if the rebase
// completes cleanly (with the branch name's base for logging).
//
// On failure (resolver error, exhausted iterations, missing prompter
// for questions, unresolved files without questions), returns false
// and the rebase remains mid-flight in the worktree; the caller
// surfaces the original conflict via the existing RebaseRescue path
// so the user can resolve manually.
func (h *Handler) tryAutoResolveRebase(
	ctx context.Context, op scriptrun.Operation, branch string, graph *spice.BranchGraph,
) (resolved bool, baseName string) {
	info, ok := graph.Lookup(branch)
	if !ok {
		h.Log.Warnf("%v: auto-resolve: branch missing from graph", branch)
		return false, ""
	}
	baseName = info.Base

	maxIters := h.resolveIterationCap()
	for range maxIters {
		conflictPaths, err := sliceutil.CollectErr(
			h.Worktree.ListFilesPaths(ctx, &git.ListFilesOptions{Unmerged: true}))
		if err != nil {
			h.Log.Warnf("%v: auto-resolve: list unmerged: %v", branch, err)
			return false, ""
		}
		if len(conflictPaths) == 0 {
			// No conflicts remaining; resume the rebase.
			if err := h.Worktree.RebaseContinue(ctx, &git.RebaseContinueOptions{Editor: "true"}); err != nil {
				return h.handleRebaseContinueResult(ctx, branch, err)
			}
			return true, baseName
		}

		resp, err := h.Resolver.Resolve(ctx, &ResolveRequest{
			Operation: op,
			Base:      baseName,
			Branch:    branch,
		})
		if err != nil {
			h.Log.Warnf("%v: auto-resolve: resolver: %v", branch, err)
			return false, ""
		}

		for _, a := range resp.Assumptions {
			h.Log.Infof("Auto-resolve: %s", a)
		}

		if len(resp.Questions) > 0 {
			if h.Prompter == nil {
				h.Log.Warnf("%v: auto-resolve: resolver returned %d question(s) but no prompter is configured",
					branch, len(resp.Questions))
				return false, ""
			}
			answers, err := h.Prompter.AskAnswers(ctx, resp.Questions)
			if err != nil {
				h.Log.Warnf("%v: auto-resolve: collect answers: %v", branch, err)
				return false, ""
			}
			if err := h.appendQAToFile(baseName, branch, resp.Questions, answers); err != nil {
				h.Log.Warnf("%v: auto-resolve: append Q&A: %v", branch, err)
				return false, ""
			}
			continue
		}

		if len(resp.UnresolvedFiles) > 0 {
			h.Log.Warnf("%v: auto-resolve: resolver reported unresolved files with no questions: %v",
				branch, resp.UnresolvedFiles)
			return false, ""
		}

		// Resolver says everything is resolved. Stage the files
		// it touched and tell git to continue the rebase.
		if err := h.Worktree.StageFiles(ctx, conflictPaths); err != nil {
			h.Log.Warnf("%v: auto-resolve: stage files: %v", branch, err)
			return false, ""
		}
		if err := h.Worktree.RebaseContinue(ctx, &git.RebaseContinueOptions{Editor: "true"}); err != nil {
			return h.handleRebaseContinueResult(ctx, branch, err)
		}
		return true, baseName
	}

	h.Log.Warnf("%v: auto-resolve: exceeded iteration cap (%d); investigate manually",
		branch, maxIters)
	return false, ""
}

// handleRebaseContinueResult interprets the error from
// [GitWorktree.RebaseContinue]. A [git.RebaseInterruptError] from
// the conflict path means a SUBSEQUENT commit in the same rebase
// also conflicted; we cannot resume from inside this loop in that
// case (the conflict-paths list and the resolver invocation need to
// be fresh), so signal failure and let the outer loop continue the
// resolution via re-entry.
//
// In practice the next RebaseRescue path will catch the user; on
// the next gs restack/gs rebase continue invocation, the auto-
// resolve loop fires again for the next conflict.
func (h *Handler) handleRebaseContinueResult(
	_ context.Context, branch string, err error,
) (bool, string) {
	rebaseErr := new(git.RebaseInterruptError)
	if errors.As(err, &rebaseErr) {
		h.Log.Infof("%v: auto-resolve: next commit also conflicted; falling through to manual resolution",
			branch)
		return false, ""
	}
	h.Log.Warnf("%v: auto-resolve: rebase --continue: %v", branch, err)
	return false, ""
}

// appendQAToFile appends Q&A pairs to the resolution file entry for
// the given (base, branch) pair. The .spice/resolutions/ directory is
// shared across gs auto-resolve features; each feature uses its own
// file (restack.json, integration.json, etc.).
func (h *Handler) appendQAToFile(
	base, branch string, questions, answers []string,
) error {
	if err := spicedir.EnsureResolutionsDir(h.RepoRoot); err != nil {
		return err
	}
	path := spicedir.ResolutionPath(h.RepoRoot, ResolutionFeatureName)
	file, err := LoadResolutionFile(path)
	if err != nil {
		return err
	}

	pair := MergePair{Ours: base, Theirs: branch}
	qa := make([]scriptrun.QAPair, 0, len(questions))
	for i, q := range questions {
		a := ""
		if i < len(answers) {
			a = answers[i]
		}
		qa = append(qa, scriptrun.QAPair{Question: q, Answer: a})
	}
	file.AppendInstructions(pair, qa...)
	return file.Save(path)
}
