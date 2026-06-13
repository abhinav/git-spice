package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/alecthomas/kong"
	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type continueCmd struct {
	Edit bool `default:"true" negatable:"" config:"rebaseContinue.edit" help:"Whether to open an editor to edit the commit message."`
}

func (*continueCmd) Help() string {
	name := cli.Name()
	return text.Dedent(fmt.Sprintf(`
		Continues an ongoing git-spice operation interrupted by
		a git rebase or merge after all conflicts have been resolved.
		For example, if '%[1]s upstack restack' gets interrupted
		because a conflict arises during the operation,
		you can resolve the conflict and run '%[1]s continue'
		to continue the operation.

		The command can be used in place of 'git rebase --continue'
		or 'git merge --continue'
		even if a git-spice operation is not currently in progress.

		Use the --no-edit flag to continue without opening an editor.
		Make --no-edit the default by setting 'spice.rebaseContinue.edit' to false
		and use --edit to override it.
	`, name))
}

func (cmd *continueCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	store *state.Store,
	parser *kong.Kong,
) error {
	return runContinue(ctx, log, wt, store, parser, cmd.Edit)
}

type abortCmd struct{}

func (*abortCmd) Help() string {
	name := cli.Name()
	return text.Dedent(fmt.Sprintf(`
		Cancels an ongoing git-spice operation that was interrupted by
		a git rebase or merge.
		For example, if '%[1]s upstack restack' encounters a conflict,
		cancel the operation with '%[1]s abort',
		going back to the state before the operation.

		The command can be used in place of 'git rebase --abort'
		or 'git merge --abort'
		even if a git-spice operation is not currently in progress.
	`, name))
}

func (cmd *abortCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	store *state.Store,
) error {
	return runAbort(ctx, log, wt, store)
}

// runContinue resumes an interrupted git-spice operation,
// whether it was interrupted by a rebase or a merge,
// then drains any recorded continuations.
//
// edit controls whether an editor is opened to amend the commit message
// produced by finishing the rebase or merge.
func runContinue(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	store *state.Store,
	parser *kong.Kong,
	edit bool,
) error {
	switch err := finishInterruptedOperation(ctx, log, wt, edit); {
	case err == nil:
		// Operation finished; drain continuations below.
	case errors.Is(err, errNoOperationInProgress):
		return errors.New("no operation to continue")
	default:
		return err
	}

	return runContinuations(ctx, log, wt, store, parser)
}

// errNoOperationInProgress reports that neither a rebase nor a merge
// is currently in progress.
var errNoOperationInProgress = errors.New("no operation in progress")

// finishInterruptedOperation finishes whichever of a rebase or merge is
// currently in progress, returning [errNoOperationInProgress] if neither is.
func finishInterruptedOperation(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	edit bool,
) error {
	switch _, err := wt.RebaseState(ctx); {
	case err == nil:
		var opts git.RebaseContinueOptions
		if !edit {
			opts.Editor = "true"
		}
		if err := wt.RebaseContinue(ctx, &opts); err != nil {
			if _, ok := errors.AsType[git.InterruptError](err); ok {
				logMoreConflicts(log)
			}
			return err
		}
		return nil
	case !errors.Is(err, git.ErrNoRebase):
		return fmt.Errorf("get rebase state: %w", err)
	}

	// Not rebasing; check for an in-progress merge.
	if _, err := wt.MergeState(ctx); err != nil {
		if !errors.Is(err, git.ErrNoMerge) {
			return fmt.Errorf("get merge state: %w", err)
		}
		return errNoOperationInProgress
	}

	if err := wt.MergeContinue(ctx, &git.MergeContinueOptions{Edit: edit}); err != nil {
		if _, ok := errors.AsType[git.InterruptError](err); ok {
			logMoreConflicts(log)
		}
		return err
	}
	return nil
}

// logMoreConflicts prints guidance for resolving the conflicts that remain
// after an attempt to continue an interrupted rebase or merge.
func logMoreConflicts(log *silog.Logger) {
	var msg strings.Builder
	fmt.Fprintf(&msg, "There are more conflicts to resolve.\n")
	fmt.Fprintf(&msg, "Resolve them and run the following command again:\n")
	fmt.Fprintf(&msg, "  %s continue\n", cli.Name())
	fmt.Fprintf(&msg, "To abort the remaining operations run:\n")
	fmt.Fprintf(&msg, "  %s abort\n", cli.Name())
	log.Error(msg.String())
}

// runContinuations drains the recorded continuation list,
// re-running each command on its branch.
//
// If any continuation fails, the remainder of the list is pushed back so a
// subsequent continue picks up where this one left off.
func runContinuations(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	store *state.Store,
	parser *kong.Kong,
) error {
	// Once we get here, we have a clean state to continue running
	// continuations on.
	// However, if any of the continuations encounters another conflict,
	// they will clear the continuation list.
	// So we'll want to grab the whole list here,
	// and push the remainder of it back on if a command fails.
	conts, err := store.TakeContinuations(ctx, cli.Name()+" continue")
	if err != nil {
		return fmt.Errorf("take continuations: %w", err)
	}

	for idx, cont := range conts {
		log.Debug("Running post-rebase operation",
			"command", strings.Join(cont.Command, " "),
			"branch", cont.Branch)
		if err := wt.CheckoutBranch(ctx, cont.Branch); err != nil {
			return fmt.Errorf("checkout branch %q: %w", cont.Branch, err)
		}

		kctx, err := parser.Parse(cont.Command)
		if err != nil {
			log.Errorf("Corrupt continuation: %q", cont.Command)
			return fmt.Errorf("parse continuation: %w", err)
		}

		if err := kctx.Run(ctx); err != nil {
			// If the command failed, it has already printed the
			// message, and appended its continuations.
			// We'll append the remainder.
			if err := store.AppendContinuations(ctx, "continue", conts[idx+1:]...); err != nil {
				return fmt.Errorf("append continuations: %w", err)
			}
			return err
		}
	}

	return nil
}

// runAbort cancels whichever of a rebase or merge is currently in progress,
// then clears any recorded continuations.
func runAbort(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	store *state.Store,
) error {
	var operationAborted bool
	switch err := abortInterruptedOperation(ctx, wt); {
	case err == nil:
		operationAborted = true
	case errors.Is(err, errNoOperationInProgress):
		// If the user ran 'git rebase --abort' or 'git merge --abort'
		// first, we will not be in the middle of an operation.
		// That's okay, still drain the continuations
		// to ensure we don't have any lingering state.
	default:
		return err
	}

	conts, err := store.TakeContinuations(ctx, cli.Name()+" abort")
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
		log.Debug("Operation aborted: will not run command",
			"command", strings.Join(cont.Command, " "),
			"branch", cont.Branch)
	}

	return nil
}

// abortInterruptedOperation aborts whichever of a rebase or merge is
// currently in progress, returning [errNoOperationInProgress] if neither is.
func abortInterruptedOperation(ctx context.Context, wt *git.Worktree) error {
	switch _, err := wt.RebaseState(ctx); {
	case err == nil:
		if err := wt.RebaseAbort(ctx); err != nil {
			return fmt.Errorf("abort rebase: %w", err)
		}
		return nil
	case !errors.Is(err, git.ErrNoRebase):
		return fmt.Errorf("get rebase state: %w", err)
	}

	// Not rebasing; check for an in-progress merge.
	if _, err := wt.MergeState(ctx); err != nil {
		if !errors.Is(err, git.ErrNoMerge) {
			return fmt.Errorf("get merge state: %w", err)
		}
		return errNoOperationInProgress
	}

	if err := wt.MergeAbort(ctx); err != nil {
		return fmt.Errorf("abort merge: %w", err)
	}
	return nil
}
