package main

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/merge"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type branchMergeCmd struct {
	merge.Options

	Branches []string `name:"branch" placeholder:"NAME" help:"Branches to merge. May be repeated." predictor:"trackedBranches"`
}

func (*branchMergeCmd) Help() string {
	return text.Dedent(`
		Merges the CR for the current branch into trunk.
		Use --branch to merge a different branch.
		Use --branch multiple times to merge multiple branches.

		Only the selected branches are merged.
		To merge a branch and its downstack,
		use 'git-spice downstack merge'.
		To merge a whole stack,
		use 'git-spice stack merge'.

		Before checking merge readiness,
		the command waits briefly for the forge to observe the pushed head.
		Then it waits for the forge to report that the CR is ready to merge.
		Use --ready-timeout to configure the maximum wait.
	`)
}

func (cmd *branchMergeCmd) AfterApply(
	ctx context.Context,
	wt *git.Worktree,
) error {
	if len(cmd.Branches) > 0 {
		return nil
	}
	branch, err := wt.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}
	cmd.Branches = []string{branch}
	return nil
}

func (cmd *branchMergeCmd) Run(
	ctx context.Context,
	store *state.Store,
	mergeHandler MergeHandler,
) error {
	if slices.Contains(cmd.Branches, store.Trunk()) {
		return errors.New("cannot merge trunk")
	}

	return mergeHandler.MergeBranch(ctx, &merge.BranchMergeRequest{
		Branches: cmd.Branches,
		Options:  &cmd.Options,
	})
}
