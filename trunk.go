package main

import (
	"context"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/ui"
)

type trunkCmd struct {
	checkoutOptions
}

func (cmd *trunkCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
) error {
	trunk := store.Trunk()
	return (&branchCheckoutCmd{
		checkoutOptions: cmd.checkoutOptions,
		Branch:          trunk,
	}).Run(ctx, log, view, repo, wt, store, svc)
}
