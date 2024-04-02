package main

import (
	"context"
	"fmt"
	"log"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
)

type branchTrackCmd struct {
	Parent string `short:"p" help:"Parent branch of this branch"`
	Name   string `arg:"" optional:"" help:"Name of the branch to track"`
}

func (cmd *branchTrackCmd) Run(ctx context.Context, log *log.Logger) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	if cmd.Name == "" {
		cmd.Name, err = repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
	}

	// TODO: prompt for init if not initialized
	store, err := state.OpenStore(ctx, repo)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}

	if cmd.Name == store.Trunk() {
		return fmt.Errorf("cannot track trunk branch")
	}

	// TODO: handle already tracking
	// TODO: auto-detect parent branch with revision matching

	if cmd.Parent == "" {
		return fmt.Errorf("missing required flag -p")
	}

	parentHash, err := repo.PeelToCommit(ctx, cmd.Parent)
	if err != nil {
		return fmt.Errorf("peel to commit: %w", err)
	}

	if err := store.SetBranch(ctx, state.SetBranchRequest{
		Name:     cmd.Name,
		Base:     cmd.Parent,
		BaseHash: parentHash,
	}); err != nil {
		return fmt.Errorf("set branch: %w", err)
	}

	log.Printf("%v: tracking with parent %v", cmd.Name, cmd.Parent)
	return nil
}
