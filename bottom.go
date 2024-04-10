package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
)

type bottomCmd struct{}

func (*bottomCmd) Run(ctx context.Context, log *log.Logger) error {
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

	current, err := repo.CurrentBranch(ctx)
	if err != nil {
		// TODO: handle not a branch
		return fmt.Errorf("get current branch: %w", err)
	}

	if current == store.Trunk() {
		return errors.New("no branches below current: already on trunk")
	}

	// TODO: store: add ListDownstack() operation.
	var bottom string
	for {
		b, err := store.LookupBranch(ctx, current)
		if err != nil {
			return fmt.Errorf("lookup %v: %w", current, err)
		}

		if b.Base.Name == store.Trunk() {
			bottom = current
			break
		}

		current = b.Base.Name
	}

	return (&checkoutCmd{Name: bottom}).Run(ctx, log)
}
