package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
)

type topCmd struct{}

func (*topCmd) Run(ctx context.Context, log *log.Logger) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	// TODO: prompt for init if not initialized
	store, err := state.OpenStore(ctx, repo, log)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}

	// TODO: ensure no uncommitted changes

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	var top string
loop:
	for {
		children, err := store.ListAbove(ctx, currentBranch)
		if err != nil {
			return fmt.Errorf("list children of %q: %w", currentBranch, err)
		}

		switch len(children) {
		case 0:
			top = currentBranch
			break loop
		case 1:
			currentBranch = children[0]
		default:
			// TODO: prompt user for which child to follow
			return fmt.Errorf("branch %q has multiple children", currentBranch)
		}
	}

	if err := repo.Checkout(ctx, top); err != nil {
		return fmt.Errorf("checkout %q: %w", top, err)
	}

	return nil
}
