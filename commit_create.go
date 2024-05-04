package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
)

type commitCreateCmd struct {
	All     bool   `short:"a" help:"Stage all changes before committing."`
	Message string `short:"m" help:"Use the given message as the commit message."`
}

func (cmd *commitCreateCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	if err := repo.Commit(ctx, git.CommitRequest{
		Message: cmd.Message,
		All:     cmd.All,
	}); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return (&upstackRestackCmd{}).Run(ctx, log, opts)
}
