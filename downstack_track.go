package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/track"
	"go.abhg.dev/gs/internal/text"
)

type downstackTrackCmd struct {
	Branch string `arg:"" optional:"" help:"Name of the branch to start tracking from" predictor:"branches"`
}

func (*downstackTrackCmd) Help() string {
	return text.Dedent(`
		Track all untracked branches in the downstack of a branch.

		Starting from the specified branch (or current branch),
		identify and track any untracked branches downstack from it,
		until reaching trunk or an already-tracked branch.
	`)
}

func (cmd *downstackTrackCmd) AfterApply(
	ctx context.Context,
	wt *git.Worktree,
) error {
	if cmd.Branch == "" {
		var err error
		cmd.Branch, err = wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
	}

	return nil
}

func (cmd *downstackTrackCmd) Run(ctx context.Context, handler TrackHandler) error {
	return handler.TrackDownstack(ctx, &track.DownstackRequest{
		Branch: cmd.Branch,
	})
}
