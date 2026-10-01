package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/handler/continuation"
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

func (cmd *rebaseAbortCmd) Run(ctx context.Context, handler ContinuationHandler) error {
	return handler.Abort(ctx, &continuation.AbortRequest{})
}
