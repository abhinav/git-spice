package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
)

type commitAmendCmd struct {
	Message string `short:"m" help:"Use the given message as the commit message."`
	NoEdit  bool   `short:"n" help:"Don't edit the commit message"`
}

func (cmd *commitAmendCmd) Run(ctx context.Context, log *log.Logger) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	// TODO: prompt for init if not initialized
	store, err := state.OpenStore(ctx, repo)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		currentBranch = "" // not a tracked branch
	}

	if err := repo.Commit(ctx, git.CommitRequest{
		Message: cmd.Message,
		Amend:   true,
		NoEdit:  cmd.NoEdit,
	}); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	// Restack upstack branches if tracked.
	_ = store
	_ = currentBranch

	return nil
}
