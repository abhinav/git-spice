package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/merge"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type branchMergeCmd struct {
	merge.Options

	Branch string `placeholder:"NAME" help:"Branch to merge" predictor:"trackedBranches"`
}

func (*branchMergeCmd) Help() string {
	return text.Dedent(`
		Merges the CR for the current branch into trunk.
		Use --branch to merge a different branch.

		The branch must be based directly on trunk.
		To merge a stacked branch, use 'gs downstack merge'.

		Before merging, waits for merge readiness:
		the forge must observe the pushed head
		and report that the CR is ready to merge.
		Use --ready-timeout to configure the maximum wait.
	`)
}

func (cmd *branchMergeCmd) AfterApply(
	ctx context.Context,
	wt *git.Worktree,
) error {
	if cmd.Branch != "" {
		return nil
	}
	branch, err := wt.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}
	cmd.Branch = branch
	return nil
}

func (cmd *branchMergeCmd) Run(
	ctx context.Context,
	store *state.Store,
	mergeHandler MergeHandler,
) error {
	if cmd.Branch == store.Trunk() {
		return errors.New("cannot merge trunk")
	}

	return mergeHandler.MergeBranch(ctx, &merge.BranchMergeRequest{
		Branch:  cmd.Branch,
		Options: &cmd.Options,
	})
}
