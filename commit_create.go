package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type commitCreateCmd struct {
	All     bool   `short:"a" help:"Stage all changes before committing."`
	Fixup   string `help:"Create a fixup commit."`
	Message string `short:"m" help:"Use the given message as the commit message."`
}

func (*commitCreateCmd) Help() string {
	return text.Dedent(`
		Staged changes are committed to the current branch.
		Branches upstack are restacked if necessary.
		Use this as a shortcut for 'git commit'
		followed by 'gs upstack restack'.
	`)
}

func (cmd *commitCreateCmd) Run(ctx context.Context, log *log.Logger, view ui.View) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	if err := repo.Commit(ctx, git.CommitRequest{
		Message: cmd.Message,
		All:     cmd.All,
		Fixup:   cmd.Fixup,
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
		Branch:    currentBranch,
		SkipStart: true,
	}).Run(ctx, log, view)
}
