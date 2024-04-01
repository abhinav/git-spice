package main

import (
	"context"
	"fmt"

	"github.com/alecthomas/kong"
	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
)

type stackSubmitCmd struct {
	DryRun bool `short:"n" help:"Don't actually submit the stack"`
}

func (cmd *stackSubmitCmd) Run(
	ctx context.Context,
	app *kong.Kong,
	log *log.Logger,
) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	head, err := repo.Head(ctx)
	if err != nil {
		return fmt.Errorf("get HEAD commit: %w", err)
	}

	app.Printf("HEAD commit is %s", head)
	return nil
}
