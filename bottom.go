package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type bottomCmd struct {
	checkoutOptions
}

func (*bottomCmd) Help() string {
	return text.Dedent(`
		Checks out the bottom-most branch in the current branch's stack.
		Use the -n flag to print the branch without checking it out.
	`)
}

func (cmd *bottomCmd) Run(
	ctx context.Context,
	log *log.Logger,
	view ui.View,
	repo *git.Repository,
	store *state.Store,
	svc *spice.Service,
) error {
	current, err := repo.CurrentBranch(ctx)
	if err != nil {
		// TODO: handle not a branch
		return fmt.Errorf("get current branch: %w", err)
	}

	if current == store.Trunk() {
		return errors.New("no branches below current: already on trunk")
	}

	bottom, err := svc.FindBottom(ctx, current)
	if err != nil {
		return fmt.Errorf("find bottom: %w", err)
	}

	return (&branchCheckoutCmd{
		checkoutOptions: cmd.checkoutOptions,
		Branch:          bottom,
	}).Run(ctx, log, view, repo, store, svc)
}
