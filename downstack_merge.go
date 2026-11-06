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

type downstackMergeCmd struct {
	merge.Options

	Branches []string `name:"branch" placeholder:"NAME" help:"Branches to start merging from. May be repeated." predictor:"trackedBranches"`
}

func (*downstackMergeCmd) Help() string {
	return text.Dedent(fmt.Sprintf(`
		Merges CRs for the current branch and all branches below it into trunk.
		Use --branch to merge the downstack of a different branch.
		Use --branch multiple times to merge downstacks of multiple branches.
		Selected branches and their downstack branches down to trunk are merged.

		For example, for the following stack:

                       ┌── D
                       │ ┌── C
                       ├─┴ B
                     ┌─┴ A
                    trunk

		The following commands have the following effects:

		    %[1]s downstack merge --branch D # merge A, D
		    %[1]s downstack merge --branch B # merge A, B
		    %[1]s downstack merge --branch C # merge A, B, C
		    %[1]s downstack merge \          # merge A, B, C, D
		                          --branch C --branch D

		Use '%[1]s stack merge' to merge a branch
		and its upstack branches in one operation.
	`, cli.Name())) + _mergeHelpCommon
}

// MergeHandler merges change requests via a forge.
type MergeHandler interface {
	MergeDownstack(ctx context.Context, req *merge.DownstackMergeRequest) error
	MergeBranch(ctx context.Context, req *merge.BranchMergeRequest) error
	MergeStack(ctx context.Context, req *merge.StackMergeRequest) error
}

func (cmd *downstackMergeCmd) AfterApply(
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

func (cmd *downstackMergeCmd) Run(
	ctx context.Context,
	store *state.Store,
	mergeHandler MergeHandler,
) error {
	if slices.Contains(cmd.Branches, store.Trunk()) {
		return errors.New("cannot merge trunk")
	}

	return mergeHandler.MergeDownstack(ctx, &merge.DownstackMergeRequest{
		Branches: cmd.Branches,
		Options:  &cmd.Options,
	})
}
