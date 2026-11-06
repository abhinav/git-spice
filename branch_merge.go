package main

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/cli"
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
	return text.Dedent(fmt.Sprintf(`
		Merges the CR for the current branch into trunk.
		Use --branch to merge a different branch.
		Use --branch multiple times to merge multiple branches.
		Only the selected branches are merged.
		All selected branches must be stacked on trunk
		or on a branch that is also selected.

		For example, for the following stack:

		       ┌── B
		     ┌─┴ A
		    trunk

		This command can merge A alone,
		or A and B together.

		    %[1]s branch merge --branch A
		    %[1]s branch merge --branch A --branch B

		It cannot merge B alone, because A is not selected:

		    %[1]s branch merge --branch B // error

		To merge multiple branches in a stack
		prefer '%[1]s downstack merge' or '%[1]s stack merge'.
	`, cli.Name())) + _mergeHelpCommon
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
