package main

import (
	"context"

	"github.com/alecthomas/kong"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/text"
)

type logShortCmd struct {
	branchLogCmd
}

func (*logShortCmd) Help() string {
	return text.Dedent(`
		Only branches that are upstack and downstack from the current
		branch are shown.
		Use with the -a/--all flag to show all tracked branches.
	`)
}

func (cmd *logShortCmd) Run(
	ctx context.Context,
	kctx *kong.Context,
	wt *git.Worktree,
	listHandler ListHandler,
) (err error) {
	return cmd.run(ctx, kctx, nil, wt, listHandler)
}
