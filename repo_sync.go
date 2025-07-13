package main

import (
	"context"

	"go.abhg.dev/gs/internal/handler/sync"
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

func (cmd *repoSyncCmd) Run(ctx context.Context, syncHandler SyncHandler) error {
	return syncHandler.SyncTrunk(ctx, &cmd.TrunkOptions)
}
