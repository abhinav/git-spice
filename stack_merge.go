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

type stackMergeCmd struct {
	merge.StackMergeOptions

	Branch string `placeholder:"NAME" help:"Branch whose stack to merge" predictor:"trackedBranches"`
}

func (*stackMergeCmd) Help() string {
	return text.Dedent(`
		Merges the CRs for the current branch's stack into trunk.
		Use --branch to merge a different branch's stack.

		The stack includes the selected branch,
		its downstack branches down to trunk,
		and every upstack branch.

		Already-merged branches are skipped automatically.
		Branches must have an open Change Request to be merged.

		Before merging, the stack is checked for branches
		whose base PR was already merged on the forge.
		Use --no-branch-check to skip this validation.

		Before each merge, waits for merge readiness:
		the forge must observe the pushed head
		and report that the CR is ready to merge.
		Use --ready-timeout to configure the maximum wait
		before failing if merge readiness is not reached.

		By default, a branch failure skips that branch's upstack descendants,
		but independent sibling branches continue.
		Use --fail-fast to stop the queue after the first branch failure.
	`)
}

func (cmd *stackMergeCmd) AfterApply(
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

func (cmd *stackMergeCmd) Run(
	ctx context.Context,
	store *state.Store,
	mergeHandler MergeHandler,
) error {
	if cmd.Branch == store.Trunk() {
		return errors.New("cannot merge trunk")
	}

	return mergeHandler.MergeStack(ctx, &merge.StackMergeRequest{
		Branch:  cmd.Branch,
		Options: &cmd.StackMergeOptions,
	})
}
