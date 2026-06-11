package main

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/branchsync"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type upstackSyncCmd struct {
	Branch string `placeholder:"NAME" help:"Branch to start at" predictor:"trackedBranches"`
	Rebase bool   `help:"On divergence, replay remote-side commits onto local."`
}

func (*upstackSyncCmd) Help() string {
	return text.Dedent(`
		Pull remote-side commits for the current branch and all
		branches upstack from it. Branches that fast-forward
		(or rebase, with --rebase) trigger an upstack restack of
		their children so the stack stays coherent.
		Use --branch to start at a different branch.
	`)
}

func (cmd *upstackSyncCmd) Run(
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

	branches := slices.Collect(graph.Upstack(cmd.Branch))
	if cmd.Branch == store.Trunk() && len(branches) > 0 && branches[0] == cmd.Branch {
		branches = branches[1:]
	}

	return syncBranches(ctx, log, branchSyncHandler, restackHandler, branches, cmd.Rebase)
}

// syncBranches runs Sync on each branch in order and restacks the
// upstack of any branch whose tip moved.
func syncBranches(
	ctx context.Context,
	log *silog.Logger,
	branchSyncHandler *branchsync.Handler,
	restackHandler RestackHandler,
	branches []string,
	rebase bool,
) error {
	mode := branchsync.ModeFastForward
	if rebase {
		mode = branchsync.ModeRebase
	}

	for _, branch := range branches {
		res, err := branchSyncHandler.Sync(ctx, branchsync.SyncRequest{Branch: branch, Mode: mode})
		if err != nil {
			if errors.Is(err, branchsync.ErrNoUpstream) {
				continue
			}
			log.Warnf("%v: sync failed: %v", branch, err)
			continue
		}
		switch res.Action {
		case branchsync.ActionClean:
			// quiet
		case branchsync.ActionFastForward:
			log.Infof("%v: fast-forwarded %s..%s", res.Branch, res.FromHash.Short(), res.ToHash.Short())
		case branchsync.ActionRebased:
			log.Infof("%v: rebased onto remote %s", res.Branch, res.ToHash.Short())
		case branchsync.ActionBehind:
			// quiet
		case branchsync.ActionDiverged:
			log.Warnf("%v: diverged from remote; pass --rebase to integrate", res.Branch)
		case branchsync.ActionSkipped:
			log.Warnf("%v: skipped (%v)", res.Branch, res.SkipReason)
		}

		if res.FromHash != res.ToHash {
			if err := restackHandler.RestackUpstack(ctx, res.Branch, nil); err != nil {
				log.Warnf("%v: upstack restack after sync failed: %v", res.Branch, err)
			}
		}
	}

	return nil
}
