package main

import (
	"context"
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/branchsync"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type downstackSyncCmd struct {
	Branch string `placeholder:"NAME" help:"Branch to start at" predictor:"trackedBranches"`
	Rebase bool   `help:"On divergence, replay remote-side commits onto local."`
}

func (*downstackSyncCmd) Help() string {
	return text.Dedent(`
		Pull remote-side commits for the current branch and all
		branches downstack from it (excluding trunk).
		Use --branch to start at a different branch.
	`)
}

func (cmd *downstackSyncCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
	branchSyncHandler *branchsync.Handler,
	restackHandler RestackHandler,
) error {
	if cmd.Branch == "" {
		currentBranch, err := wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = currentBranch
	}

	graph, err := svc.BranchGraph(ctx, nil)
	if err != nil {
		return fmt.Errorf("build branch graph: %w", err)
	}

	branches := slices.Collect(graph.Downstack(cmd.Branch))
	// Downstack is [branch, child1, child2, ..., trunk-adjacent]; we want
	// to sync bottom-up so children pick up updated bases.
	slices.Reverse(branches)
	// Drop trunk if present.
	trunk := store.Trunk()
	branches = slices.DeleteFunc(branches, func(b string) bool { return b == trunk })

	return syncBranches(ctx, log, branchSyncHandler, restackHandler, branches, cmd.Rebase)
}
