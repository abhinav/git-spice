package main

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
)

type upstackRestackCmd struct{}

func (*upstackRestackCmd) Run(ctx context.Context, log *zerolog.Logger) error {
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

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	upstacks, err := store.ListUpstack(ctx, currentBranch)
	if err != nil {
		return fmt.Errorf("get upstack branches: %w", err)
	}

	for _, upstack := range upstacks {
		// Trunk never needs to be restacked.
		if upstack == store.Trunk() {
			continue
		}

		err := (&branchRestackCmd{Name: upstack}).Run(ctx, log)
		if err != nil {
			return fmt.Errorf("restack upstack %v: %w", upstack, err)
		}
	}

	// On success, check out the original branch.
	if err := repo.Checkout(ctx, currentBranch); err != nil {
		return fmt.Errorf("checkout branch %v: %w", currentBranch, err)
	}

	return nil
}
