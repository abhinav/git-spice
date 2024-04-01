package main

import (
	"context"
	"fmt"
	"log"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
)

type branchDownCmd struct{}

func (*branchDownCmd) Run(ctx context.Context, log *log.Logger) error {
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

	branchRes, err := store.GetBranch(ctx, currentBranch)
	if err != nil {
		log.Printf("get branch %q: %v", currentBranch, err)
		return fmt.Errorf("branch %q is not being tracked", currentBranch)
	}

	if branchRes.Base == store.Trunk() {
		log.Printf("exiting stack: moving to trunk: %v", store.Trunk())
	}

	// TODO: warn about top of stack when moving to upstream branch.
	if err := repo.Checkout(ctx, branchRes.Base); err != nil {
		return fmt.Errorf("checkout %q: %w", branchRes.Base, err)
	}

	return nil
}
