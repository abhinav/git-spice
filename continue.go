package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/handler/continuation"
	"go.abhg.dev/gs/internal/text"
)

// ContinuationHandler resumes or cancels an interrupted operation.
type ContinuationHandler interface {
	Continue(ctx context.Context, req *continuation.ContinueRequest) error
	Abort(ctx context.Context, req *continuation.AbortRequest) error
}

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

func (cmd *continueCmd) Run(ctx context.Context, handler ContinuationHandler) error {
	return handler.Continue(ctx, &continuation.ContinueRequest{Edit: cmd.Edit})
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

func (cmd *abortCmd) Run(ctx context.Context, handler ContinuationHandler) error {
	return handler.Abort(ctx, &continuation.AbortRequest{})
}
