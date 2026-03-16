package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/autostash"
	"go.abhg.dev/gs/internal/handler/sync"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/text"
)

type repoSyncCmd struct {
	sync.TrunkOptions
}

func (*repoSyncCmd) Help() string {
	return text.Dedent(`
		Branches with merged Change Requests
		will be deleted after syncing.

		The repository must have a remote associated for syncing.
		A prompt will ask for one if the repository
		was not initialized with a remote.
	`)
}

// SyncHandler is a subset of sync.Handler.
type SyncHandler interface {
	SyncTrunk(ctx context.Context, opts *sync.TrunkOptions) error
}

func (cmd *repoSyncCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	syncHandler SyncHandler,
	autostashHandler AutostashHandler,
) (retErr error) {
	currentBranch, err := wt.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	cleanup, err := autostashHandler.BeginAutostash(
		ctx, &autostash.Options{
			Message:   "git-spice: autostash before sync",
			ResetMode: autostash.ResetHard,
			Branch:    currentBranch,
		},
	)
	if err != nil {
		return err
	}
	defer cleanup(&retErr)

	if err := syncHandler.SyncTrunk(
		ctx, &cmd.TrunkOptions,
	); err != nil {
		return err
	}

	if err := wt.CheckoutBranch(
		ctx, currentBranch,
	); err != nil {
		log.Warn(
			"Could not restore original branch",
			"branch", currentBranch,
			"error", err,
		)
	}

	return nil
}
