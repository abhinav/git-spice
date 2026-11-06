package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type downstackRestackCmd struct {
	Branch string `help:"Branch to restack the downstack of" placeholder:"NAME" predictor:"trackedBranches"`
}

func (*downstackRestackCmd) Help() string {
	return text.Dedent(`
		The current branch and all branches below it until trunk
		are rebased on top of their respective bases,
		ensuring a linear history.

		Use --branch to start at a different branch.
	`)
}

func (cmd *downstackRestackCmd) AfterApply(ctx context.Context, wt *git.Worktree) error {
	if cmd.Branch == "" {
		currentBranch, err := wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = currentBranch
	}
	return nil
}

func (cmd *downstackRestackCmd) Run(
	ctx context.Context,
	store *state.Store,
	handler RestackHandler,
) error {
	if cmd.Branch == store.Trunk() {
		return errors.New("nothing to restack below trunk")
	}

	return handler.RestackDownstack(ctx, &restack.DownstackRequest{
		Branch: cmd.Branch,
	})
}
