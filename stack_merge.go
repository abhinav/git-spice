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

type stackMergeCmd struct {
	merge.Options

	Branches []string `name:"branch" placeholder:"NAME" help:"Branches whose stacks to merge. May be repeated." predictor:"trackedBranches"`
}

func (*stackMergeCmd) Help() string {
	return text.Dedent(fmt.Sprintf(`
		Merges CRs for the current branch's full stack into trunk.
		Use --branch to merge a different branch's stack.
		Use --branch multiple times to merge independent stacks.
		A stack includes the selected branch,
		its downstack branches down to trunk,
		and every upstack branch.

		For example, for the following stack:

                         ┌── E
                       ┌─┴ D
                       │ ┌── C
                       ├─┴ B
                     ┌─┴ A
                    trunk

		The following commands have the following effects:

		    %[1]s stack merge --branch A # merge A, B, C, D, E
		    %[1]s stack merge --branch B # merge A, B, C
		    %[1]s stack merge --branch D # merge A, D, E
	`, cli.Name())) + _mergeHelpCommon
}

func (cmd *stackMergeCmd) AfterApply(
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

func (cmd *stackMergeCmd) Run(
	ctx context.Context,
	store *state.Store,
	mergeHandler MergeHandler,
) error {
	if slices.Contains(cmd.Branches, store.Trunk()) {
		return errors.New("cannot merge trunk")
	}

	return mergeHandler.MergeStack(ctx, &merge.StackMergeRequest{
		Branches: cmd.Branches,
		Options:  &cmd.Options,
	})
}
