// Package continuation resumes or cancels a git-spice operation
// interrupted by a Git rebase or merge.
package continuation

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/autostash"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
)

//go:generate mockgen -package continuation -destination mocks_test.go . GitWorktree,Store,CommandParser,Command

// GitWorktree is a subset of the git.Worktree interface.
type GitWorktree interface {
	RebaseState(ctx context.Context) (*git.RebaseState, error)
	RebaseContinue(ctx context.Context, opts *git.RebaseContinueOptions) error
	RebaseAbort(ctx context.Context) error

	MergeState(ctx context.Context) (*git.MergeState, error)
	MergeContinue(ctx context.Context, opts *git.MergeContinueOptions) error
	MergeAbort(ctx context.Context) error

	CheckoutBranch(ctx context.Context, branch string) error
}

var _ GitWorktree = (*git.Worktree)(nil)

// Store is a subset of the state.Store interface.
type Store interface {
	AppendContinuations(ctx context.Context, msg string, conts ...state.Continuation) error
	TakeContinuations(ctx context.Context, msg string) ([]state.Continuation, error)
}

var _ Store = (*state.Store)(nil)

// Command is a parsed git-spice command line.
type Command interface {
	Run(ctx context.Context) error
}

// CommandParser parses the command line recorded on a [state.Continuation].
type CommandParser interface {
	Parse(args []string) (Command, error)
}

// Handler resumes or cancels an interrupted git-spice operation.
type Handler struct {
	Log      *silog.Logger // required
	Worktree GitWorktree   // required
	Store    Store         // required
	Parser   CommandParser // required
}

// ErrNoOperation indicates that neither a rebase nor a merge
// is currently in progress.
var ErrNoOperation = errors.New("no operation in progress")

// ContinueRequest configures [Handler.Continue].
type ContinueRequest struct {
	// Edit opens an editor for the commit message
	// that finishes the rebase or merge.
	Edit bool
}

// Continue finishes an interrupted rebase or merge,
// then runs the recorded continuations.
func (h *Handler) Continue(ctx context.Context, req *ContinueRequest) error {
	req = cmp.Or(req, &ContinueRequest{})

	switch err := h.finishInterruptedOperation(ctx, req.Edit); {
	case err == nil:
	case errors.Is(err, ErrNoOperation):
		return errors.New("no operation to continue")
	default:
		return err
	}

	return h.runContinuations(ctx)
}

// finishInterruptedOperation finishes whichever of a rebase or merge is
// currently in progress, returning [ErrNoOperation] if neither is.
func (h *Handler) finishInterruptedOperation(ctx context.Context, edit bool) error {
	switch _, err := h.Worktree.RebaseState(ctx); {
	case err == nil:
		var opts git.RebaseContinueOptions
		if !edit {
			opts.Editor = "true"
		}
		if err := h.Worktree.RebaseContinue(ctx, &opts); err != nil {
			if _, ok := errors.AsType[git.InterruptError](err); ok {
				h.logMoreConflicts()
			}
			return err
		}
		return nil
	case !errors.Is(err, git.ErrNoRebase):
		return fmt.Errorf("get rebase state: %w", err)
	}

	if _, err := h.Worktree.MergeState(ctx); err != nil {
		if !errors.Is(err, git.ErrNoMerge) {
			return fmt.Errorf("get merge state: %w", err)
		}
		return ErrNoOperation
	}

	if err := h.Worktree.MergeContinue(ctx, &git.MergeContinueOptions{Edit: edit}); err != nil {
		if _, ok := errors.AsType[git.InterruptError](err); ok {
			h.logMoreConflicts()
		}
		return err
	}
	return nil
}

func (h *Handler) logMoreConflicts() {
	var msg strings.Builder
	fmt.Fprintf(&msg, "There are more conflicts to resolve.\n")
	fmt.Fprintf(&msg, "Resolve them and run the following command again:\n")
	fmt.Fprintf(&msg, "  %s continue\n", cli.Name())
	fmt.Fprintf(&msg, "To abort the remaining operations run:\n")
	fmt.Fprintf(&msg, "  %s abort\n", cli.Name())
	h.Log.Error(msg.String())
}

// runContinuations runs each recorded continuation on its branch.
func (h *Handler) runContinuations(ctx context.Context) error {
	// Once we get here, we have a clean state to continue running
	// continuations on.
	// However, if any of the continuations encounters another conflict,
	// they will clear the continuation list.
	// So we'll want to grab the whole list here,
	// and push the remainder of it back on if a command fails.
	conts, err := h.Store.TakeContinuations(ctx, cli.Name()+" continue")
	if err != nil {
		return fmt.Errorf("take continuations: %w", err)
	}

	for idx, cont := range conts {
		h.Log.Debug("Running post-rebase operation",
			"command", strings.Join(cont.Command, " "),
			"branch", cont.Branch)
		if err := h.Worktree.CheckoutBranch(ctx, cont.Branch); err != nil {
			return fmt.Errorf("checkout branch %q: %w", cont.Branch, err)
		}

		cmd, err := h.Parser.Parse(cont.Command)
		if err != nil {
			h.Log.Errorf("Corrupt continuation: %q", cont.Command)
			return fmt.Errorf("parse continuation: %w", err)
		}

		if err := cmd.Run(ctx); err != nil {
			// If the command failed, it has already printed the
			// message, and appended its continuations.
			// We'll append the remainder.
			if err := h.Store.AppendContinuations(ctx, cli.Name()+" continue", conts[idx+1:]...); err != nil {
				return fmt.Errorf("append continuations: %w", err)
			}
			return err
		}
	}

	return nil
}

// AbortRequest configures [Handler.Abort].
type AbortRequest struct{}

// Abort cancels an interrupted rebase or merge
// and drops the recorded continuations.
// Autostashed changes are restored instead of dropped
// so that aborting does not lose uncommitted work.
func (h *Handler) Abort(ctx context.Context, _ *AbortRequest) error {
	var operationAborted bool
	switch err := h.abortInterruptedOperation(ctx); {
	case err == nil:
		operationAborted = true
	case errors.Is(err, ErrNoOperation):
		// If the user ran 'git rebase --abort' or 'git merge --abort'
		// first, we will not be in the middle of an operation.
		// That's okay, still drain the continuations
		// to ensure we don't have any lingering state.
	default:
		return err
	}

	conts, err := h.Store.TakeContinuations(ctx, cli.Name()+" abort")
	if err != nil {
		return fmt.Errorf("take continuations: %w", err)
	}

	// Make sure that *something* happened from the user's perspective.
	// If we didn't abort an operation, and we didn't delete a continuation,
	// then this was a no-op, which this command should not be.
	if len(conts) == 0 && !operationAborted {
		return errors.New("no operation to abort")
	}

	for _, cont := range conts {
		if !autostash.IsRestoreCommand(cont.Command) {
			h.Log.Debug("Operation aborted: will not run command",
				"command", strings.Join(cont.Command, " "),
				"branch", cont.Branch)
			continue
		}

		if err := h.restoreAutostash(ctx, cont); err != nil {
			return err
		}
	}

	return nil
}

func (h *Handler) restoreAutostash(ctx context.Context, cont state.Continuation) error {
	h.Log.Debug("Operation aborted: restoring autostashed changes",
		"command", strings.Join(cont.Command, " "),
		"branch", cont.Branch)
	// Best-effort: if the stash cannot be applied here,
	// the restore keeps it in the stash list.
	if err := h.Worktree.CheckoutBranch(ctx, cont.Branch); err != nil {
		h.Log.Warn("Could not return to the branch the operation started from",
			"branch", cont.Branch, "error", err)
	}

	cmd, err := h.Parser.Parse(cont.Command)
	if err != nil {
		h.Log.Errorf("Corrupt continuation: %q", cont.Command)
		return fmt.Errorf("parse continuation: %w", err)
	}

	if err := cmd.Run(ctx); err != nil {
		return fmt.Errorf("restore autostashed changes: %w", err)
	}
	return nil
}

// abortInterruptedOperation aborts whichever of a rebase or merge is
// currently in progress, returning [ErrNoOperation] if neither is.
func (h *Handler) abortInterruptedOperation(ctx context.Context) error {
	switch _, err := h.Worktree.RebaseState(ctx); {
	case err == nil:
		if err := h.Worktree.RebaseAbort(ctx); err != nil {
			return fmt.Errorf("abort rebase: %w", err)
		}
		return nil
	case !errors.Is(err, git.ErrNoRebase):
		return fmt.Errorf("get rebase state: %w", err)
	}

	if _, err := h.Worktree.MergeState(ctx); err != nil {
		if !errors.Is(err, git.ErrNoMerge) {
			return fmt.Errorf("get merge state: %w", err)
		}
		return ErrNoOperation
	}

	if err := h.Worktree.MergeAbort(ctx); err != nil {
		return fmt.Errorf("abort merge: %w", err)
	}
	return nil
}
