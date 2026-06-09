package main

import (
	"context"
	"fmt"

	"github.com/alecthomas/kong"
	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
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

func (cmd *rebaseContinueCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	store *state.Store,
	parser *kong.Kong,
) error {
	// 'gs rebase continue' shares its implementation with 'gs continue'
	// so that a user mid-merge who runs the older command still resumes
	// the operation correctly.
	return runContinue(ctx, log, wt, store, parser, cmd.Edit)
}
