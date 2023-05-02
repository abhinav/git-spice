package main

import (
	"context"
	"fmt"
	"log"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
)

type repoInitCmd struct {
	Trunk string `help:"The name of the trunk branch"`
}

func (cmd *repoInitCmd) Run(ctx context.Context, log *log.Logger) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	if cmd.Trunk == "" {
		cmd.Trunk = "main" // TODO: prompt for trunk
	}

	_, err = state.InitStore(ctx, state.InitStoreRequest{
		Repository: repo,
		Trunk:      cmd.Trunk,
	})
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}

	return nil
}
