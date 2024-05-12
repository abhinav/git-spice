package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/text"
)

type commitAmendCmd struct {
	Message string `short:"m" help:"Use the given message as the commit message."`
	NoEdit  bool   `short:"n" help:"Don't edit the commit message"`
}

func (*commitAmendCmd) Help() string {
	return text.Dedent(`
		Amends the last commit with the staged changes,
		restacking upstack branches if necessary.
		Use this to keep upstack branches in sync
		as you update a branch in the middle of the stack.
	`)
}

func (cmd *commitAmendCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	if err := repo.Commit(ctx, git.CommitRequest{
		Message: cmd.Message,
		Amend:   true,
		NoEdit:  cmd.NoEdit,
	}); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return (&upstackRestackCmd{}).Run(ctx, log, opts)
}
