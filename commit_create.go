package main

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
)

type commitCreateCmd struct {
	All     bool   `short:"a" help:"Stage all changes before committing."`
	Message string `short:"m" help:"Use the given message as the commit message."`
}

func (cmd *commitCreateCmd) Run(ctx context.Context, log *zerolog.Logger) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	// TODO: prompt for init if not initialized
	store, err := state.OpenStore(ctx, repo, log)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		currentBranch = "" // not a tracked branch
	}

	if err := repo.Commit(ctx, git.CommitRequest{
		Message: cmd.Message,
		All:     cmd.All,
	}); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	// Restack upstack branches if tracked.
	_ = store
	_ = currentBranch

	return nil
}
