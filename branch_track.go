package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/track"
	"go.abhg.dev/gs/internal/text"
)

type branchTrackCmd struct {
	Base   string `short:"b" placeholder:"BRANCH" help:"Base branch this merges into" predictor:"trackedBranches"`
	Branch string `arg:"" optional:"" help:"Name of the branch to track" predictor:"branches"`
}

func (*branchTrackCmd) Help() string {
	return text.Dedent(`
		A branch must be tracked to be able to run gs operations on it.
		Use 'gs branch create' to automatically track new branches.

		The base is guessed by comparing against other tracked branches.
		Use --base to specify a base explicitly.
	`)
}

// TrackHandler allows tracking branches.
type TrackHandler interface {
	AddBranch(context.Context, *track.AddBranchRequest) error
}

func (cmd *branchTrackCmd) AfterApply(
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

func (cmd *branchTrackCmd) Run(
	ctx context.Context,
	handler TrackHandler,
) error {
	return handler.AddBranch(ctx, &track.AddBranchRequest{
		Branch: cmd.Branch,
		Base:   cmd.Base,
	})
}
