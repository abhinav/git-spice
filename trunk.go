package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/ui"
)

type trunkCmd struct {
	checkoutOptions
}

func (cmd *trunkCmd) Run(ctx context.Context, log *log.Logger, view ui.View) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	store, err := ensureStore(ctx, repo, log, view)
	if err != nil {
		return err
	}

	trunk := store.Trunk()
	return (&branchCheckoutCmd{
		checkoutOptions: cmd.checkoutOptions,
		Branch:          trunk,
	}).Run(ctx, log, view)
}
