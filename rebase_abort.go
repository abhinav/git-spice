package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type rebaseAbortCmd struct{}

func (*rebaseAbortCmd) Help() string {
	name := cli.Name()
	return text.Dedent(fmt.Sprintf(`
		Cancels an ongoing git-spice operation that was interrupted by
		a git rebase.
		For example, if '%[1]s upstack restack' encounters a conflict,
		cancel the operation with '%[1]s rebase abort'
		(or its shorthand '%[1]s rba'),
		going back to the state before the rebase.

		The command can be used in place of 'git rebase --abort'
		even if a git-spice operation is not currently in progress.
	`, name))
}

func (cmd *rebaseAbortCmd) Run(
	ctx context.Context,
	wt *git.Worktree,
	log *silog.Logger,
	store *state.Store,
) error {
	// 'gs rebase abort' shares its implementation with 'gs abort'
	// so that a user mid-merge who runs the older command still aborts
	// the operation correctly.
	return runAbort(ctx, log, wt, store)
}
