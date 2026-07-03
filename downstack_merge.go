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

type downstackMergeCmd struct {
	merge.DownstackMergeOptions

	Branches []string `name:"branch" placeholder:"NAME" help:"Branches to start merging from. May be repeated." predictor:"trackedBranches"`
}

func (*downstackMergeCmd) Help() string {
	return text.Dedent(`
		Merges the current branch and all branches below it
		into trunk via the forge API, bottom-up.
		Use --branch to start at a different branch.
		Use --branch multiple times to merge multiple downstacks.

		Each selected branch expands to that branch
		and its downstack branches down to trunk.
		Overlapping downstacks are merged once.

		This command acts as a local merge queue:
		it merges one Change Request,
		waits for that merge to finish,
		restacks and updates the next Change Request,
		waits for merge readiness on the updated Change Request,
		and then repeats the process.

		For a stack like this:

		    main <- feature1 <- feature2 <- feature3

		Running from feature3 merges in this order:

		    feature1, feature2, feature3

		Already-merged branches are skipped automatically.
		Branches must have an open Change Request to be merged.

		Before merging, the downstack is checked for branches
		whose base PR was already merged on the forge.
		Use --no-branch-check to skip this validation.

		Before checking merge readiness,
		the command waits briefly for the forge to observe the pushed head.
		Then it waits for the forge to report that the CR is ready to merge.
		Use --ready-timeout to configure the maximum wait
		(default: 30m, 0 means fail immediately if not ready).

		Between merges, the command waits for each merge
		to complete, restacks and updates the next PR,
		waits for merge readiness on the updated PR,
		and syncs merged branch cleanup.
	`)
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
		Options:  &cmd.DownstackMergeOptions,
	})
}
