package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
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
	`)
}

// RestackHandler implements high level restack operations.
type RestackHandler interface {
	RestackUpstack(ctx context.Context, req *restack.UpstackRequest) error
	RestackDownstack(ctx context.Context, req *restack.DownstackRequest) error
	Restack(context.Context, *restack.Request) (int, error)
	RestackStack(ctx context.Context, req *restack.StackRequest) error
	RestackBranch(ctx context.Context, req *restack.BranchRequest) error
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

func (cmd *upstackRestackCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	store *state.Store,
	handler RestackHandler,
) error {
	if err := verifyRestackFromTrunk(log, view, store, cmd.Branch, "upstack"); err != nil {
		return err
	}

	return handler.RestackUpstack(ctx, &restack.UpstackRequest{
		Branch:  cmd.Branch,
		Options: &cmd.UpstackOptions,
	})
}
