package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/handler/continuation"
	"go.abhg.dev/gs/internal/text"
)

type rebaseContinueCmd struct {
	Edit bool `default:"true" negatable:"" config:"rebaseContinue.edit" help:"Whether to open an editor to edit the commit message."`
}

func (*rebaseContinueCmd) Help() string {
	name := cli.Name()
	return text.Dedent(fmt.Sprintf(`
		Continues an ongoing git-spice operation interrupted by
		a git rebase after all conflicts have been resolved.
		For example, if '%[1]s upstack restack' gets interrupted
		because a conflict arises during the rebase,
		you can resolve the conflict and run '%[1]s rebase continue'
		(or its shorthand '%[1]s rbc') to continue the operation.

		The command can be used in place of 'git rebase --continue'
		even if a git-spice operation is not currently in progress.

		Use the --no-edit flag to continue without opening an editor.
		Make --no-edit the default by setting 'spice.rebaseContinue.edit' to false
		and use --edit to override it.
	`, name))
}

func (cmd *rebaseContinueCmd) Run(ctx context.Context, handler ContinuationHandler) error {
	return handler.Continue(ctx, &continuation.ContinueRequest{Edit: cmd.Edit})
}
