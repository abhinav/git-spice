package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
)

type branchDeleteCmd struct {
	Name  string `arg:"" optional:"" help:"Name of the branch to delete"`
	Force bool   `short:"f" help:"Force deletion of the branch"`
}

func (cmd *branchDeleteCmd) Run(ctx context.Context, log *zerolog.Logger) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	store, err := state.OpenStore(ctx, repo, log)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}

	// TODO: prompt for branch if not provided or not an exact match
	if cmd.Name == "" {
		return errors.New("branch name is required")
	}

	if err := store.ForgetBranch(ctx, cmd.Name); err != nil {
		return fmt.Errorf("forget branch: %w", err)
	}

	if err := repo.DeleteBranch(ctx, cmd.Name, git.BranchDeleteOptions{
		Force: cmd.Force,
	}); err != nil {
		// may have already been deleted
		log.Warn().Err(err).Msg("error deleting branch")
	}

	panic("TODO: restack the upstack")
}
