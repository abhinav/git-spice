package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

func TestStore_Anchors(t *testing.T) {
	ctx := t.Context()
	db := storage.NewDB(make(storage.MapBackend))
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    db,
		Trunk: "main",
	})
	require.NoError(t, err)

	// Empty registry: only the canonical trunk is a trunk.
	assert.True(t, store.IsTrunk("main"))
	assert.False(t, store.IsTrunk("feature"))
	assert.Equal(t, "main", store.TrunkFor("/any/path"))
	assert.Equal(t, "main", store.TrunkFor(""))
	assert.Empty(t, store.Anchors())

	require.NoError(t, store.RegisterAnchor(ctx, state.Anchor{
		Branch:   "wt-b",
		Worktree: "/wt/b",
	}))
	require.NoError(t, store.RegisterAnchor(ctx, state.Anchor{
		Branch:   "wt-a",
		Worktree: "/wt/a",
		Base:     "feat-x",
	}))

	// Any registered anchor is now a trunk; resolution is per-worktree.
	assert.True(t, store.IsTrunk("main"))
	assert.True(t, store.IsTrunk("wt-a"))
	assert.True(t, store.IsTrunk("wt-b"))
	assert.False(t, store.IsTrunk("feature"))
	assert.Equal(t, "wt-a", store.TrunkFor("/wt/a"))
	assert.Equal(t, "wt-b", store.TrunkFor("/wt/b"))
	assert.Equal(t, "main", store.TrunkFor("/wt/unknown"))

	// Anchors is sorted by branch name, and the base round-trips.
	assert.Equal(t, []state.Anchor{
		{Branch: "wt-a", Worktree: "/wt/a", Base: "feat-x"},
		{Branch: "wt-b", Worktree: "/wt/b"},
	}, store.Anchors())

	// Registry survives a reopen of the store.
	reopened, err := state.OpenStore(ctx, db, nil)
	require.NoError(t, err)
	assert.True(t, reopened.IsTrunk("wt-a"))
	assert.Equal(t, "wt-b", reopened.TrunkFor("/wt/b"))
	assert.Equal(t, []state.Anchor{
		{Branch: "wt-a", Worktree: "/wt/a", Base: "feat-x"},
		{Branch: "wt-b", Worktree: "/wt/b"},
	}, reopened.Anchors())

	// Unregister drops the branch from the registry.
	require.NoError(t, store.UnregisterAnchor(ctx, "wt-a"))
	assert.False(t, store.IsTrunk("wt-a"))
	assert.Equal(t, "main", store.TrunkFor("/wt/a"))
	assert.Equal(t, []state.Anchor{
		{Branch: "wt-b", Worktree: "/wt/b"},
	}, store.Anchors())
}

func TestStore_AnchorIsGraphRoot(t *testing.T) {
	ctx := t.Context()
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    storage.NewDB(make(storage.MapBackend)),
		Trunk: "main",
	})
	require.NoError(t, err)

	require.NoError(t, store.RegisterAnchor(ctx, state.Anchor{
		Branch:   "wt-a",
		Worktree: "/wt/a",
	}))

	// A branch may be stacked directly on an anchor: the anchor is a
	// valid root base even though it is not itself tracked.
	tx := store.BeginBranchTx()
	require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
		Name: "feature",
		Base: "wt-a",
	}))
	require.NoError(t, tx.Commit(ctx, "track feature on anchor"))

	info, err := store.LookupBranch(ctx, "feature")
	require.NoError(t, err)
	assert.Equal(t, "wt-a", info.Base)

	// An anchor may not itself be tracked or deleted.
	tx = store.BeginBranchTx()
	assert.ErrorIs(t, tx.Upsert(ctx, state.UpsertRequest{
		Name: "wt-a",
		Base: "main",
	}), state.ErrTrunk)
	assert.ErrorIs(t, tx.Delete(ctx, "wt-a"), state.ErrTrunk)
	require.NoError(t, tx.Commit(ctx, "no op"))
}

func TestStore_RegisterAnchor_rejectsCanonicalTrunk(t *testing.T) {
	ctx := t.Context()
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    storage.NewDB(make(storage.MapBackend)),
		Trunk: "main",
	})
	require.NoError(t, err)

	err = store.RegisterAnchor(ctx, state.Anchor{Branch: "main"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "canonical trunk")
}

func TestStore_RegisterAnchor_rejectsSecondAnchorForWorktree(t *testing.T) {
	ctx := t.Context()
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    storage.NewDB(make(storage.MapBackend)),
		Trunk: "main",
	})
	require.NoError(t, err)

	require.NoError(t, store.RegisterAnchor(ctx, state.Anchor{
		Branch:   "wt-a",
		Worktree: "/wt/a",
	}))

	// A second, different anchor branch for the same worktree is
	// refused: a worktree resolves to exactly one trunk, so two anchors
	// would make TrunkFor ambiguous.
	err = store.RegisterAnchor(ctx, state.Anchor{
		Branch:   "wt-a2",
		Worktree: "/wt/a",
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "already has anchor")

	// The registry is unchanged: the worktree still resolves to its
	// original anchor and the rejected branch is not a trunk.
	assert.Equal(t, "wt-a", store.TrunkFor("/wt/a"))
	assert.False(t, store.IsTrunk("wt-a2"))
}

func TestStore_RegisterAnchor_allowsSameBranchWorktreeMove(t *testing.T) {
	ctx := t.Context()
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    storage.NewDB(make(storage.MapBackend)),
		Trunk: "main",
	})
	require.NoError(t, err)

	require.NoError(t, store.RegisterAnchor(ctx, state.Anchor{
		Branch:   "wt-a",
		Worktree: "/wt/old",
	}))

	// Re-registering the same anchor branch at a new path is an
	// idempotent move, not a second anchor: worktrees are advisory and
	// can relocate.
	require.NoError(t, store.RegisterAnchor(ctx, state.Anchor{
		Branch:   "wt-a",
		Worktree: "/wt/new",
	}))

	assert.Equal(t, "wt-a", store.TrunkFor("/wt/new"))
	assert.Equal(t, "main", store.TrunkFor("/wt/old"))
}

func TestStore_TrunkFor_deterministic(t *testing.T) {
	ctx := t.Context()
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    storage.NewDB(make(storage.MapBackend)),
		Trunk: "main",
	})
	require.NoError(t, err)

	require.NoError(t, store.RegisterAnchor(ctx, state.Anchor{
		Branch:   "wt-a",
		Worktree: "/wt/a",
	}))
	require.NoError(t, store.RegisterAnchor(ctx, state.Anchor{
		Branch:   "wt-b",
		Worktree: "/wt/b",
	}))

	// Resolution is stable across repeated calls regardless of Go's
	// randomized map iteration order.
	for range 50 {
		assert.Equal(t, "wt-a", store.TrunkFor("/wt/a"))
		assert.Equal(t, "wt-b", store.TrunkFor("/wt/b"))
	}
}
