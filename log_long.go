package main

import (
	"context"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/text"
)

type logLongCmd struct {
	branchLogCmd
}

func (*logLongCmd) Help() string {
	return text.Dedent(`
		Only branches that are upstack and downstack from the current
		branch are shown.
		Use with the -a/--all flag to show all tracked branches.
	`)
}

func (cmd *logLongCmd) Run(
	ctx context.Context,
	wt *git.Worktree,
	listHandler ListHandler,
) (err error) {
	return cmd.run(ctx, &branchLogOptions{
		Commits: true,
	}, wt, listHandler)
}
