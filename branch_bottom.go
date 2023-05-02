package main

import (
	"context"
	"fmt"
	"log"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
)

type branchBottomCmd struct{}

func (*branchBottomCmd) Run(ctx context.Context, log *log.Logger) error {
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

	var bottom string
loop:
	for {
		children, err := store.ListBranchChildren(ctx, currentBranch)
		if err != nil {
			return fmt.Errorf("list children of %q: %w", currentBranch, err)
		}

		switch len(children) {
		case 0:
			bottom = currentBranch
			break loop
		case 1:
			currentBranch = children[0]
		default:
			// TODO: prompt user for which child to follow
			return fmt.Errorf("branch %q has multiple children", currentBranch)
		}
	}

	if err := repo.Checkout(ctx, bottom); err != nil {
		return fmt.Errorf("checkout %q: %w", bottom, err)
	}

	return nil
}
