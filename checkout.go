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

	// A branch needs to be restacked if
	// a) it's tracked by gs; and
	// b) its merge base with its base branch
	//    is not its base branch's head
	if b, err := store.LookupBranch(ctx, cmd.Name); err == nil {
		mergeBase, err := repo.MergeBase(ctx, cmd.Name, b.Base.Name)
		if err != nil {
			log.Warn("Could not look up merge base for branch with its base",
				"branch", cmd.Name,
				"base", b.Base.Name,
				"err", err)
		} else {
			baseHash, err := repo.PeelToCommit(ctx, b.Base.Name)
			if err != nil {
				log.Warnf("%v: base branch %v may not exist: %v", cmd.Name, b.Base.Name, err)
			} else if baseHash != mergeBase {
				log.Warnf("%v: needs to be restacked: run 'gs branch restack'", cmd.Name)
			}
		}
	}

	if err := repo.Checkout(ctx, cmd.Name); err != nil {
		return fmt.Errorf("checkout %q: %w", cmd.Name, err)
	}

	return nil
}
