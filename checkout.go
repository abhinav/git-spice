package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
)

type checkoutCmd struct {
	Name string `arg:"" optional:"" help:"Name of the branch to delete"`
}

func (cmd *checkoutCmd) Run(ctx context.Context, log *log.Logger) error {
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

	// TODO: prompt for branch if not provided or not an exact match
	if cmd.Name == "" {
		return errors.New("branch name is required")
	}

	checkNeedsRestack(ctx, repo, store, log, cmd.Name)
	if err := repo.Checkout(ctx, cmd.Name); err != nil {
		return fmt.Errorf("checkout %q: %w", cmd.Name, err)
	}

	return nil
}
