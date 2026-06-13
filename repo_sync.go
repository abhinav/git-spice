package main

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/handler/branchsync"
	"go.abhg.dev/gs/internal/handler/sync"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type pullBranchesMode string

const (
	pullBranchesOff         pullBranchesMode = "off"
	pullBranchesFastForward pullBranchesMode = "fastForward"
	pullBranchesRebase      pullBranchesMode = "rebase"
)

type repoSyncCmd struct {
	sync.TrunkOptions

	PullBranches pullBranchesMode `default:"fastForward" config:"repoSync.pullBranches" enum:"off,fastForward,rebase" help:"How to integrate remote-side commits on tracked stack branches. 'off' skips the per-branch pull; 'fastForward' (default) advances safe branches and skips diverged ones; 'rebase' replays diverged branches' local commits on top of the remote."`
}

func (*repoSyncCmd) Help() string {
	return text.Dedent(`
		Branches with merged Change Requests
		will be deleted after syncing.

		The repository must have a remote associated for syncing.
		A prompt will ask for one if the repository
		was not initialized with a remote.

		Branches above merged and deleted branches
		are retargeted to the trunk branch.
		Run with --restack to also restack them and their upstacks.
		Run with --restack=aboves to only restack direct upstacks
		of deleted branches, leaving higher branches in place.

		After the trunk sync, every other tracked branch is also
		checked against its remote: if the remote has new commits
		(typically from a CI bot) and local has not moved past the
		last-pushed hash, the local branch is fast-forwarded so the
		next 'gs submit' will not be rejected with "stale info".
		Pass --no-pull-branches to skip this step.
	`)
}

// SyncHandler is a subset of sync.Handler.
type SyncHandler interface {
	SyncTrunk(ctx context.Context, opts *sync.TrunkOptions) error
}

func (cmd *repoSyncCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	store *state.Store,
	syncHandler SyncHandler,
	branchSyncHandler *branchsync.Handler,
	restackHandler RestackHandler,
) error {
	if err := syncHandler.SyncTrunk(ctx, &cmd.TrunkOptions); err != nil {
		return err
	}

	if cmd.PullBranches == pullBranchesOff {
		return nil
	}
	rebase := cmd.PullBranches == pullBranchesRebase

	var branches []string
	for branch, err := range store.ListBranches(ctx) {
		if err != nil {
			return fmt.Errorf("list tracked branches: %w", err)
		}
		branches = append(branches, branch)
	}
	slices.Sort(branches)

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
		case branchsync.ActionFastForward:
			log.Infof("%v: fast-forwarded %s..%s", res.Branch, res.FromHash.Short(), res.ToHash.Short())
			// The branch moved; children built on the old hash need
			// to be rebased onto the new tip.
			if err := restackHandler.RestackUpstack(ctx, res.Branch, nil); err != nil {
				log.Warnf("%v: upstack restack after sync failed: %v", res.Branch, err)
			}
		case branchsync.ActionDiverged:
			log.Warnf("%v: diverged from remote; run 'gs branch sync --rebase' to integrate", res.Branch)
		case branchsync.ActionSkipped:
			log.Warnf("%v: skipped (%v)", res.Branch, res.SkipReason)
		}
	}

	return nil
}
