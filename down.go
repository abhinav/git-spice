package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
)

type downCmd struct{}

func (*downCmd) Run(ctx context.Context, log *log.Logger) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	store, err := ensureStore(ctx, repo, log)
	if err != nil {
		return err
	}

	// TODO: ensure no uncommitted changes

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	if currentBranch == store.Trunk() {
		log.Error("there are no more branches downstack")
		return fmt.Errorf("already on trunk: %v", currentBranch)
	}

	branchRes, err := store.LookupBranch(ctx, currentBranch)
	if err != nil {
		return fmt.Errorf("branch %q is not being tracked: %w", currentBranch, err)
	}

	if branchRes.Base.Name == store.Trunk() {
		log.Infof("exiting stack: moving to trunk: %v", store.Trunk())
	}

	if err := repo.Checkout(ctx, branchRes.Base.Name); err != nil {
		return fmt.Errorf("checkout %q: %w", branchRes.Base, err)
	}

	return nil
}
