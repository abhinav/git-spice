package main

import (
	"context"
	"fmt"
	"log"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
)

type branchUpCmd struct{}

func (*branchUpCmd) Run(ctx context.Context, log *log.Logger) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	// TODO: prompt for init if not initialized
	store, err := state.OpenStore(ctx, repo)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}

	// TODO: ensure no uncommitted changes

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	children, err := store.UpstackDirect(ctx, currentBranch)
	if err != nil {
		return fmt.Errorf("list children of %q: %w", currentBranch, err)
	}

	switch len(children) {
	case 0:
		return fmt.Errorf("%v: no branches found upstack", currentBranch)
	case 1:
		if err := repo.Checkout(ctx, children[0]); err != nil {
			return fmt.Errorf("checkout %q: %w", children[0], err)
		}
	default:
		// TODO: prompt user for which child to checkout
		return fmt.Errorf("not implemented: multiple children")
	}

	return nil
}
