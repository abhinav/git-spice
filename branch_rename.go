package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
)

type branchRenameCmd struct {
	Name string `arg:"" optional:"" help:"New name of the branch"`
}

func (cmd *branchRenameCmd) Run(ctx context.Context, log *log.Logger) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	// TODO: prompt for a name if not provided
	if cmd.Name == "" {
		return errors.New("branch name is required")
	}

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	// TODO: prompt for init if not initialized
	store, err := state.OpenStore(ctx, repo)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}

	if _, err := store.GetBranch(ctx, currentBranch); err != nil {
		// branch not tracked; just rename using vanilla git
		panic("TODO")
	}

	panic("TODO: atomically move the branch file and update its children")
}
