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

	current, err := repo.CurrentBranch(ctx)
	if err != nil {
		// TODO: handle not a branch
		return fmt.Errorf("get current branch: %w", err)
	}

	trunk := store.Trunk()
	if current == trunk {
		return fmt.Errorf("%v: no branches found downstack", current)
	}

	b, err := store.LookupBranch(ctx, current)
	if err != nil {
		return fmt.Errorf("look up branch %v: %w", current, err)
	}

	below := b.Base
	if below == trunk {
		log.Infof("exiting stack: moving to trunk: %v", trunk)
	}

	return (&checkoutCmd{Name: below}).Run(ctx, log)
}
