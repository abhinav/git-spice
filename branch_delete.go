package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"go.abhg.dev/gs/internal/git"
)

type branchDeleteCmd struct {
	Name  string `arg:"" optional:"" help:"Name of the branch to delete"`
	Force bool   `short:"f" help:"Force deletion of the branch"`
}

func (cmd *branchDeleteCmd) Run(ctx context.Context, log *log.Logger) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	// TODO: prompt for branch if not provided or not an exact match
	if cmd.Name == "" {
		return errors.New("branch name is required")
	}

	if err := repo.DeleteBranch(ctx, cmd.Name, git.BranchDeleteOptions{
		Force: cmd.Force,
	}); err != nil {
		return fmt.Errorf("delete branch %q: %w", cmd.Name, err)
	}

	panic("TODO: restack the upstack")
}
