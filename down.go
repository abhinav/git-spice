package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
)

type downCmd struct{}

func (*downCmd) Run(ctx context.Context, log *log.Logger) error {
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

	branchRes, err := store.LookupBranch(ctx, currentBranch)
	if err != nil {
		return fmt.Errorf("branch %q is not being tracked: %w", currentBranch, err)
	}

	if branchRes.Base.Name == store.Trunk() {
		log.Infof("exiting stack: moving to trunk: %v", store.Trunk())
	}

	// TODO: warn about top of stack when moving to upstream branch.
	if err := repo.Checkout(ctx, branchRes.Base.Name); err != nil {
		return fmt.Errorf("checkout %q: %w", branchRes.Base, err)
	}

	return nil
}
