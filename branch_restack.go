package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/text"
)

type branchRestackCmd struct {
	Branch string `placeholder:"NAME" help:"Branch to restack" predictor:"trackedBranches"`
}

func (*branchRestackCmd) Help() string {
	return text.Dedent(`
		The current branch will be rebased onto its base,
		ensuring a linear history.
		Use --branch to target a different branch.
	`)
}

func (cmd *branchRestackCmd) AfterApply(ctx context.Context, wt *git.Worktree) error {
	if cmd.Branch == "" {
		currentBranch, err := wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = currentBranch
	}
	return nil
}

func (cmd *branchRestackCmd) Run(ctx context.Context, handler RestackHandler) error {
	return handler.RestackBranch(ctx, cmd.Branch)
}
