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

type stackSyncCmd struct {
	Branch string `placeholder:"NAME" help:"Branch to identify the stack" predictor:"trackedBranches"`
	Rebase bool   `help:"On divergence, replay remote-side commits onto local."`
}

func (*stackSyncCmd) Help() string {
	return text.Dedent(`
		Pull remote-side commits for every branch in the current
		stack (downstack and upstack of the named branch), bottom-up.
		Use --branch to identify a different stack.
	`)
}

func (cmd *stackSyncCmd) Run(
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

	// Downstack bottom-up, then upstack top-down (excluding the
	// duplicated start branch).
	downs := slices.Collect(graph.Downstack(cmd.Branch))
	slices.Reverse(downs)
	ups := slices.Collect(graph.Upstack(cmd.Branch))
	if len(ups) > 0 && ups[0] == cmd.Branch {
		ups = ups[1:]
	}

	trunk := store.Trunk()
	branches := make([]string, 0, len(downs)+len(ups))
	branches = append(branches, downs...)
	branches = append(branches, ups...)
	branches = slices.DeleteFunc(branches, func(b string) bool { return b == trunk })

	return syncBranches(ctx, log, branchSyncHandler, restackHandler, branches, cmd.Rebase)
}
