package main

import (
	"context"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
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
	log *silog.Logger,
	repo *git.Repository,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
	forges *forge.Registry,
) (err error) {
	return cmd.run(ctx, &branchLogOptions{
		Log:     log,
		Commits: true,
	}, repo, wt, store, svc, forges)
}
