package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/branchsync"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/text"
)

type branchSyncCmd struct {
	Branch string `placeholder:"NAME" help:"Branch to sync" predictor:"trackedBranches"`
	Rebase bool   `help:"On divergence (both local and remote have new commits since the last push), replay remote-side commits onto local. Conflicts surface as a normal interrupted rebase; resume with 'gs rebase --continue'."`
}

func (*branchSyncCmd) Help() string {
	return text.Dedent(`
		Pull remote-side commits added to a tracked branch since the
		last push (typically by a CI bot like autofix-ci).

		The branch is fast-forwarded only if local has no commits past
		the last push and the remote has moved forward. Diverged
		branches (both sides have new commits) are reported and left
		unchanged in this release; use 'gs upstack restack' after
		moving the parent if you need to integrate them manually.

		Children of a fast-forwarded branch will need a restack;
		'gs upstack restack' from this branch will pick that up.
	`)
}

func (cmd *branchSyncCmd) AfterApply(ctx context.Context, wt *git.Worktree) error {
	if cmd.Branch == "" {
		currentBranch, err := wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = currentBranch
	}
	return nil
}

func (cmd *branchSyncCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	handler *branchsync.Handler,
) error {
	req := branchsync.SyncRequest{Branch: cmd.Branch}
	if cmd.Rebase {
		req.Mode = branchsync.ModeRebase
	}
	res, err := handler.Sync(ctx, req)
	if err != nil {
		return err
	}

	switch res.Action {
	case branchsync.ActionClean:
		log.Infof("%v: already in sync", res.Branch)
	case branchsync.ActionFastForward:
		log.Infof("%v: fast-forwarded %s..%s", res.Branch, res.FromHash.Short(), res.ToHash.Short())
	case branchsync.ActionRebased:
		log.Infof("%v: rebased onto remote %s", res.Branch, res.ToHash.Short())
	case branchsync.ActionBehind:
		log.Infof("%v: ahead of remote; nothing to pull", res.Branch)
	case branchsync.ActionDiverged:
		log.Warnf("%v: diverged from remote; pass --rebase to integrate remote-side commits", res.Branch)
	case branchsync.ActionSkipped:
		log.Warnf("%v: skipped (%v)", res.Branch, res.SkipReason)
	}

	return nil
}
