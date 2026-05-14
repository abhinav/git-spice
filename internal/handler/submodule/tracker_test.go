package submodule_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/submodule"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

type fakeWorktree struct {
	subs     []git.Submodule
	branches map[string]string // path → branch
	detached map[string]bool   // path → true if detached
}

func (f *fakeWorktree) Submodules(
	_ context.Context,
) ([]git.Submodule, error) {
	return f.subs, nil
}

func (f *fakeWorktree) SubmoduleCurrentBranch(
	_ context.Context, path string,
) (string, error) {
	if f.detached[path] {
		return "", git.ErrDetachedHead
	}
	return f.branches[path], nil
}

func TestTracker_ResolveAssociations(t *testing.T) {
	t.Parallel()

	wt := &fakeWorktree{
		subs: []git.Submodule{
			{Path: "libs/core", URL: "https://example.com/core"},
			{Path: "libs/util", URL: "https://example.com/util"},
			{Path: "vendor/ext", URL: "https://example.com/ext"},
		},
		branches: map[string]string{
			"libs/core":  "feat-core",
			"libs/util":  "feat-util",
			"vendor/ext": "main",
		},
	}

	tracker := submodule.Tracker{
		Log:      silog.Nop(),
		Worktree: wt,
	}

	assocs, err := tracker.ResolveAssociations(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []submodule.BranchAssociation{
		{Path: "libs/core", Branch: "feat-core"},
		{Path: "libs/util", Branch: "feat-util"},
		{Path: "vendor/ext", Branch: "main"},
	}, assocs)
}

func TestTracker_ResolveAssociations_excluded(t *testing.T) {
	t.Parallel()

	wt := &fakeWorktree{
		subs: []git.Submodule{
			{Path: "libs/core"},
			{Path: "libs/excluded"},
		},
		branches: map[string]string{
			"libs/core":     "feat-core",
			"libs/excluded": "feat-excluded",
		},
	}

	tracker := submodule.Tracker{
		Log:      silog.Nop(),
		Worktree: wt,
		Exclude:  []string{"libs/excluded"},
	}

	assocs, err := tracker.ResolveAssociations(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []submodule.BranchAssociation{
		{Path: "libs/core", Branch: "feat-core"},
	}, assocs)
}

func TestTracker_ResolveAssociations_detachedSkipped(
	t *testing.T,
) {
	t.Parallel()

	wt := &fakeWorktree{
		subs: []git.Submodule{
			{Path: "libs/core"},
			{Path: "libs/detached"},
		},
		branches: map[string]string{
			"libs/core": "feat-core",
		},
		detached: map[string]bool{
			"libs/detached": true,
		},
	}

	tracker := submodule.Tracker{
		Log:      silog.Nop(),
		Worktree: wt,
	}

	assocs, err := tracker.ResolveAssociations(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []submodule.BranchAssociation{
		{Path: "libs/core", Branch: "feat-core"},
	}, assocs)
}

func TestTracker_ResolveAssociations_noSubmodules(
	t *testing.T,
) {
	t.Parallel()

	wt := &fakeWorktree{}
	tracker := submodule.Tracker{
		Log:      silog.Nop(),
		Worktree: wt,
	}

	assocs, err := tracker.ResolveAssociations(t.Context())
	require.NoError(t, err)
	assert.Empty(t, assocs)
}

func TestTracker_RecordBranchState(t *testing.T) {
	t.Parallel()

	wt := &fakeWorktree{
		subs: []git.Submodule{
			{Path: "libs/core"},
			{Path: "libs/util"},
		},
		branches: map[string]string{
			"libs/core": "feat-core",
			"libs/util": "feat-util",
		},
	}

	ctx := t.Context()
	db := storage.NewDB(make(storage.MapBackend))
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    db,
		Trunk: "main",
	})
	require.NoError(t, err)

	// Create the branch first so RecordBranchState can upsert.
	tx := store.BeginBranchTx()
	require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
		Name: "feature-x",
		Base: "main",
	}))
	require.NoError(t, tx.Commit(ctx, "track branch"))

	tracker := submodule.Tracker{
		Log:      silog.Nop(),
		Worktree: wt,
		Store:    store,
	}

	require.NoError(t,
		tracker.RecordBranchState(ctx, "feature-x"),
	)

	resp, err := store.LookupBranch(ctx, "feature-x")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"libs/core": "feat-core",
		"libs/util": "feat-util",
	}, resp.Submodules)
}

func TestTracker_RecordBranchState_noSubmodules(
	t *testing.T,
) {
	t.Parallel()

	ctx := t.Context()
	db := storage.NewDB(make(storage.MapBackend))
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    db,
		Trunk: "main",
	})
	require.NoError(t, err)

	tracker := submodule.Tracker{
		Log:      silog.Nop(),
		Worktree: &fakeWorktree{},
		Store:    store,
	}

	// No submodules means no-op; should not error.
	require.NoError(t,
		tracker.RecordBranchState(ctx, "feature-x"),
	)
}

// Ensure fakeWorktree implements the interface.
var _ submodule.GitWorktree = (*fakeWorktree)(nil)

func TestTracker_RecordWithInheritance_inheritsThenOverlays(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := storage.NewDB(make(storage.MapBackend))
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    db,
		Trunk: "main",
	})
	require.NoError(t, err)

	// Set up the parent branch with two recorded submodules.
	tx := store.BeginBranchTx()
	require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
		Name: "parent",
		Base: "main",
		Submodules: map[string]string{
			"libs/core": "feat-core",
			"libs/util": "feat-util",
		},
	}))
	require.NoError(t, tx.Commit(ctx, "set up parent"))

	// Current worktree has libs/util on a different branch.
	wt := &fakeWorktree{
		subs: []git.Submodule{
			{Path: "libs/core"},
			{Path: "libs/util"},
		},
		branches: map[string]string{
			"libs/core": "feat-core",
			"libs/util": "feat-util-v2",
		},
	}

	tracker := submodule.Tracker{
		Log:      silog.Nop(),
		Worktree: wt,
		Store:    store,
	}

	// Pre-create the child branch.
	tx = store.BeginBranchTx()
	require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
		Name: "child",
		Base: "parent",
	}))
	require.NoError(t, tx.Commit(ctx, "create child"))

	require.NoError(t,
		tracker.RecordWithInheritance(ctx, "child", "parent"),
	)

	resp, err := store.LookupBranch(ctx, "child")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"libs/core": "feat-core",    // inherited from parent
		"libs/util": "feat-util-v2", // overlayed by current state
	}, resp.Submodules)
}

func TestTracker_RecordWithInheritance_noParentRecord(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := storage.NewDB(make(storage.MapBackend))
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    db,
		Trunk: "main",
	})
	require.NoError(t, err)

	wt := &fakeWorktree{
		subs: []git.Submodule{{Path: "libs/core"}},
		branches: map[string]string{
			"libs/core": "feat-core",
		},
	}

	tracker := submodule.Tracker{
		Log:      silog.Nop(),
		Worktree: wt,
		Store:    store,
	}

	tx := store.BeginBranchTx()
	require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
		Name: "feature-x",
		Base: "main",
	}))
	require.NoError(t, tx.Commit(ctx, "create branch"))

	// Parent "main" is not tracked in store, so no inheritance.
	require.NoError(t,
		tracker.RecordWithInheritance(ctx, "feature-x", "main"),
	)

	resp, err := store.LookupBranch(ctx, "feature-x")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"libs/core": "feat-core",
	}, resp.Submodules)
}
