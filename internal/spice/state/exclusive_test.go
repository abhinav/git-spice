package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

func TestStore_Exclusive(t *testing.T) {
	ctx := t.Context()
	db := storage.NewDB(make(storage.MapBackend))
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    db,
		Trunk: "main",
	})
	require.NoError(t, err)

	// A fresh repository is not in exclusive mode.
	assert.False(t, store.InExclusiveMode())
	assert.Empty(t, store.ParkedWorktrees())

	worktrees := []state.ParkedWorktree{
		{Path: "/wt/a", Branch: "feat-a", Head: "hasha", Anchor: "anchor-a"},
		{Path: "/wt/b", Head: "hashb"}, // detached
	}
	require.NoError(t, store.Park(ctx, worktrees))

	assert.True(t, store.InExclusiveMode())
	assert.Equal(t, worktrees, store.ParkedWorktrees())

	// The manifest survives a reopen of the store: this is what makes
	// park/restore resumable across a crash.
	reopened, err := state.OpenStore(ctx, db, nil)
	require.NoError(t, err)
	assert.True(t, reopened.InExclusiveMode())
	assert.Equal(t, worktrees, reopened.ParkedWorktrees())

	// Unpark leaves exclusive mode.
	require.NoError(t, store.Unpark(ctx))
	assert.False(t, store.InExclusiveMode())
	assert.Empty(t, store.ParkedWorktrees())

	// Unpark is idempotent and the cleared state round-trips.
	require.NoError(t, store.Unpark(ctx))
	cleared, err := state.OpenStore(ctx, db, nil)
	require.NoError(t, err)
	assert.False(t, cleared.InExclusiveMode())
}
