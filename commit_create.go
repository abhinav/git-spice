package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/text"
)

type commitCreateCmd struct {
	All     bool   `short:"a" help:"Stage all changes before committing."`
	Message string `short:"m" help:"Use the given message as the commit message."`
}

func (*commitCreateCmd) Help() string {
	return text.Dedent(`
		Commits the staged changes to the current branch,
		restacking upstack branches if necessary.
		Use this to keep upstack branches in sync
		as you update a branch in the middle of the stack.
	`)
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

	if _, err := repo.RebaseState(ctx); err == nil {
		// In the middle of a rebase.
		// Don't restack upstack branches.
		return nil
	}

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		// No restack needed if we're in a detached head state.
		if errors.Is(err, git.ErrDetachedHead) {
			return nil
		}
		return fmt.Errorf("get current branch: %w", err)
	}

	return (&upstackRestackCmd{
		Name:   currentBranch,
		NoBase: true,
	}).Run(ctx, log, opts)
}
