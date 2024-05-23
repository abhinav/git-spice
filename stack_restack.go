package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
)

type stackRestackCmd struct{}

func (*stackRestackCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	store, err := ensureStore(ctx, repo, log, opts)
	if err != nil {
		return err
	}

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	svc := spice.NewService(repo, store, log)
	stack, err := svc.ListStack(ctx, currentBranch)
	if err != nil {
		return fmt.Errorf("list stack: %w", err)
	}

loop:
	for _, branch := range stack {
		// Trunk never needs to be restacked.
		if branch == store.Trunk() {
			continue loop
		}

		res, err := svc.Restack(ctx, branch)
		if err != nil {
			switch {
			case errors.Is(err, spice.ErrAlreadyRestacked):
				// Log the "does not need to be restacked" message
				// only for branches that are not the current branch.
				if branch != currentBranch {
					log.Infof("%v: branch does not need to be restacked.", branch)
				}
				continue loop
			default:
				return fmt.Errorf("restack branch: %w", err)
			}
		}

		log.Infof("%v: restacked on %v", branch, res.Base)
	}

	// On success, check out the original branch.
	if err := repo.Checkout(ctx, currentBranch); err != nil {
		return fmt.Errorf("checkout branch %v: %w", currentBranch, err)
	}

	return nil
}
