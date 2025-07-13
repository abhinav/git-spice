package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/text"
)

type stackRestackCmd struct {
	Branch string `help:"Branch to restack the stack of" placeholder:"NAME" predictor:"trackedBranches"`
}

func (*stackRestackCmd) Help() string {
	return text.Dedent(`
		All branches in the current stack are rebased on top of their
		respective bases, ensuring a linear history.

		Use --branch to rebase the stack of a different branch.
	`)
}

func (cmd *stackRestackCmd) AfterApply(ctx context.Context, wt *git.Worktree) error {
	if cmd.Branch == "" {
		currentBranch, err := wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = currentBranch
	}
	return nil
}

func (cmd *stackRestackCmd) Run(ctx context.Context, handler RestackHandler) error {
	return handler.RestackStack(ctx, cmd.Branch)
}
