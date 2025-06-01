package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type rebaseAbortCmd struct{}

func (*rebaseAbortCmd) Help() string {
	return text.Dedent(`
		Cancels an ongoing git-spice operation that was interrupted by
		a git rebase.
		For example, if 'gs upstack restack' encounters a conflict,
		cancel the operation with 'gs rebase abort'
		(or its shorthand 'gs rba'),
		going back to the state before the rebase.

		The command can be used in place of 'git rebase --abort'
		even if a git-spice operation is not currently in progress.
	`)
}

func (cmd *rebaseAbortCmd) Run(
	ctx context.Context,
	wt *git.Worktree,
	log *silog.Logger,
	store *state.Store,
) error {
	var wasRebasing bool
	if _, err := wt.RebaseState(ctx); err != nil {
		if !errors.Is(err, git.ErrNoRebase) {
			return fmt.Errorf("get rebase state: %w", err)
		}
		// If the user ran 'git rebase --abort' first,
		// we will not be in the middle of a rebase operation.
		// That's okay, still drain the continuations
		// to ensure we don't have any lingering state.
	} else {
		wasRebasing = true
		if err := wt.RebaseAbort(ctx); err != nil {
			return fmt.Errorf("abort rebase: %w", err)
		}
	}

	conts, err := store.TakeContinuations(ctx, "gs rebase abort")
	if err != nil {
		return fmt.Errorf("take rebase continuations: %w", err)
	}

	// Make sure that *something* happened from the user's perspective.
	// If we didn't abort a rebase, and we didn't delete a continuation,
	// then this was a no-op, which this command should not be.
	if len(conts) == 0 && !wasRebasing {
		return errors.New("no operation to abort")
	}

	for _, cont := range conts {
		log.Debug("Rebase aborted: will not run command",
			"command", strings.Join(cont.Command, " "),
			"branch", cont.Branch)
	}

	return nil
}
