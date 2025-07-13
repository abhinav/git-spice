package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/text"
)

type upstackRestackCmd struct {
	restack.UpstackOptions

	Branch string `help:"Branch to restack the upstack of" placeholder:"NAME" predictor:"trackedBranches"`
}

func (*upstackRestackCmd) Help() string {
	return text.Dedent(`
		The current branch and all branches above it
		are rebased on top of their respective bases,
		ensuring a linear history.
		Use --branch to start at a different branch.
		Use --skip-start to skip the starting branch,
		but still rebase all branches above it.

		The target branch defaults to the current branch.
		If run from the trunk branch,
		all managed branches will be restacked.
	`)
}

// RestackHandler implements high level restack operations.
type RestackHandler interface {
	RestackUpstack(ctx context.Context, branch string, opts *restack.UpstackOptions) error
	RestackStack(ctx context.Context, branch string) error
}

func (cmd *upstackRestackCmd) AfterApply(ctx context.Context, wt *git.Worktree) error {
	if cmd.Branch == "" {
		currentBranch, err := wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = currentBranch
	}
	return nil
}

func (cmd *upstackRestackCmd) Run(ctx context.Context, handler RestackHandler) error {
	return handler.RestackUpstack(ctx, cmd.Branch, &cmd.UpstackOptions)
}
