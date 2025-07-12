package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/checkout"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type bottomCmd struct {
	checkout.Options
}

func (*bottomCmd) Help() string {
	return text.Dedent(`
		Checks out the bottom-most branch in the current branch's stack.
		Use the -n flag to print the branch without checking it out.
	`)
}

func (cmd *bottomCmd) Run(
	ctx context.Context,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
	checkoutHandler CheckoutHandler,
) error {
	current, err := wt.CurrentBranch(ctx)
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

	return checkoutHandler.CheckoutBranch(ctx, &checkout.Request{
		Branch:  bottom,
		Options: &cmd.Options,
	})
}
