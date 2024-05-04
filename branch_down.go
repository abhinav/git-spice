package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
)

type branchDownCmd struct{}

func (*branchDownCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
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

	current, err := repo.CurrentBranch(ctx)
	if err != nil {
		// TODO: handle not a branch
		return fmt.Errorf("get current branch: %w", err)
	}

	trunk := store.Trunk()
	if current == trunk {
		return fmt.Errorf("%v: no branches found downstack", current)
	}

	b, err := store.Lookup(ctx, current)
	if err != nil {
		return fmt.Errorf("look up branch %v: %w", current, err)
	}

	below := b.Base
	if below == trunk {
		log.Infof("exiting stack: moving to trunk: %v", trunk)
	}

	return (&branchCheckoutCmd{Name: below}).Run(ctx, log, opts)
}
