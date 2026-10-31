package state_test

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/statetest"
	"go.abhg.dev/gs/internal/spice/state/storage"
	"pgregory.net/rapid"
)

func TestBranchTxUpsertErrors(t *testing.T) {
	ctx := t.Context()
	db := storage.NewDB(make(storage.MapBackend))
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    db,
		Trunk: "main",
	})
	require.NoError(t, err)

	t.Run("NewBranchNoBase", func(t *testing.T) {
		ctx := t.Context()
		tx := store.BeginBranchTx()
		err := tx.Upsert(ctx, state.UpsertRequest{
			Name: "foo",
		})
		assert.ErrorContains(t, err, "new branch must have a base")
		require.NoError(t, tx.Commit(ctx, "no op"))

		_, err = store.LookupBranch(ctx, "foo")
		require.ErrorIs(t, err, state.ErrNotExist)
	})

	t.Run("NewBranchUnknownBase", func(t *testing.T) {
		ctx := t.Context()
		tx := store.BeginBranchTx()
		err := tx.Upsert(ctx, state.UpsertRequest{
			Name: "foo",
			Base: "unknown",
		})
		assert.ErrorContains(t, err, "branch unknown is not tracked")
		require.NoError(t, tx.Commit(ctx, "no op"))

		_, err = store.LookupBranch(ctx, "foo")
		require.Error(t, err)
		require.ErrorIs(t, err, state.ErrNotExist)
	})

	// Add a couple branches to work with.
	{
		tx := store.BeginBranchTx()
		require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
			Name: "foo",
			Base: "main",
		}))
		require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
			Name: "bar",
			Base: "foo",
		}))
		require.NoError(t, tx.Commit(ctx, "add foo and bar"))
	}

	t.Run("Cycle", func(t *testing.T) {
		ctx := t.Context()
		tx := store.BeginBranchTx()
		err := tx.Upsert(ctx, state.UpsertRequest{
			Name: "foo",
			Base: "bar",
		})
		assert.ErrorContains(t, err, `would create a cycle`)
		assert.ErrorContains(t, err, `foo -> bar -> foo`)
		require.NoError(t, tx.Commit(ctx, "no op"))

		foo, err := store.LookupBranch(ctx, "foo")
		require.NoError(t, err)
		assert.Equal(t, "main", foo.Base)
	})

	t.Run("DeletedBase", func(t *testing.T) {
		ctx := t.Context()
		{
			tx := store.BeginBranchTx()
			require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
				Name: "baz",
				Base: "main",
			}))
			require.NoError(t, tx.Commit(ctx, "add baz"))
		}

		tx := store.BeginBranchTx()
		require.NoError(t, tx.Delete(ctx, "baz"))
		err := tx.Upsert(ctx, state.UpsertRequest{
			Name: "qux",
			Base: "baz",
		})
		assert.ErrorContains(t, err, `branch baz is not tracked`)
		require.NoError(t, tx.Commit(ctx, "delete baz"))

		_, err = store.LookupBranch(ctx, "baz")
		require.ErrorIs(t, err, state.ErrNotExist)
	})
}

func TestBranchTx_Upsert_retryAfterReadError(t *testing.T) {
	ctx := t.Context()
	mem := make(storage.MapBackend)
	store, err := state.InitStore(ctx, state.InitStoreRequest{DB: storage.NewDB(mem), Trunk: "main"})
	require.NoError(t, err)
	mem["branches/feature"] = []byte(`{`)

	tx := store.BeginBranchTx()
	err = tx.Upsert(ctx, state.UpsertRequest{Name: "feature", BaseHash: "updated"})
	require.Error(t, err)

	mem["branches/feature"] = []byte(`{"base":{"name":"main","hash":"initial"},"change":{"shamhub":{"number":42}}}`)
	require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{Name: "feature", BaseHash: "updated"}))
	require.NoError(t, tx.Commit(ctx, "update repaired branch"))
	branch, err := store.LookupBranch(ctx, "feature")
	require.NoError(t, err)
	assert.Equal(t, "main", branch.Base)
	assert.Equal(t, "updated", branch.BaseHash.String())
	assert.Equal(t, "shamhub", branch.ChangeForge)
	assert.JSONEq(t, `{"number":42}`, branch.ChangeMetadata.String())
}

func TestBranchTxDelete(t *testing.T) {
	ctx := t.Context()
	db := storage.NewDB(make(storage.MapBackend))
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    db,
		Trunk: "main",
	})
	require.NoError(t, err)

	t.Run("MissingBranch", func(t *testing.T) {
		ctx := t.Context()
		tx := store.BeginBranchTx()
		err := tx.Delete(ctx, "")
		assert.ErrorContains(t, err, "branch name is required")
		require.NoError(t, tx.Commit(ctx, "no op"))
	})

	t.Run("TrunkNotAllowed", func(t *testing.T) {
		ctx := t.Context()
		tx := store.BeginBranchTx()
		err := tx.Delete(ctx, "main")
		assert.ErrorIs(t, err, state.ErrTrunk)
		require.NoError(t, tx.Commit(ctx, "no op"))
	})

	t.Run("UnknownBranch", func(t *testing.T) {
		ctx := t.Context()
		tx := store.BeginBranchTx()
		err := tx.Delete(ctx, "unknown")
		assert.ErrorIs(t, err, state.ErrNotExist)
	})

	// Add a couple branches to work with.
	{
		tx := store.BeginBranchTx()
		require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
			Name: "foo",
			Base: "main",
		}))
		require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
			Name: "bar",
			Base: "foo",
		}))
		require.NoError(t, tx.Commit(ctx, "add foo and bar"))
	}

	t.Run("HasAboves", func(t *testing.T) {
		ctx := t.Context()
		tx := store.BeginBranchTx()
		err := tx.Delete(ctx, "foo")
		assert.ErrorContains(t, err, "needed by bar")
		require.NoError(t, tx.Commit(ctx, "no op"))

		_, err = store.LookupBranch(ctx, "foo")
		require.NoError(t, err)
	})

	t.Run("UpsertAndDelete", func(t *testing.T) {
		ctx := t.Context()
		tx := store.BeginBranchTx()
		require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
			Name: "baz",
			Base: "main",
		}))
		require.NoError(t, tx.Delete(ctx, "baz"))
		require.NoError(t, tx.Commit(ctx, "no op"))

		_, err := store.LookupBranch(ctx, "baz")
		require.ErrorIs(t, err, state.ErrNotExist)
	})
}

func TestBranchTx_Delete_pendingGraph(t *testing.T) {
	ctx := t.Context()
	store := statetest.NewMemoryStore(t, "main", "", nil)
	require.NoError(t, statetest.UpdateBranch(ctx, store, &statetest.UpdateRequest{
		Upserts: []state.UpsertRequest{
			{Name: "foo", Base: "main"},
			{Name: "bar", Base: "foo"},
			{Name: "baz", Base: "main"},
		},
		Message: "seed graph",
	}))

	tx := store.BeginBranchTx()
	require.NoError(t, tx.Rename(ctx, "baz", "renamed"))
	require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{Name: "bar", Base: "renamed"}))
	require.NoError(t, tx.Delete(ctx, "foo"))
	assert.ErrorContains(t, tx.Delete(ctx, "renamed"), "needed by bar")
	require.NoError(t, tx.Delete(ctx, "bar"))
	require.NoError(t, tx.Delete(ctx, "renamed"))
	require.NoError(t, tx.Commit(ctx, "remove graph"))

	names, err := sliceutil.CollectErr(store.ListBranches(ctx))
	require.NoError(t, err)
	assert.Empty(t, names)
}

func TestBranchTx_UpsertMany_orderedChanges(t *testing.T) {
	ctx := t.Context()
	store := statetest.NewMemoryStore(t, "main", "", nil)
	tx := store.BeginBranchTx()
	require.NoError(t, tx.UpsertMany(ctx, []state.UpsertRequest{
		{Name: "base", Base: "main", BaseHash: "initial"},
		{Name: "child", Base: "base"},
		{Name: "base", BaseHash: "updated"},
	}))
	require.NoError(t, tx.Commit(ctx, "track stack"))
	base, err := store.LookupBranch(ctx, "base")
	require.NoError(t, err)
	assert.Equal(t, "main", base.Base)
	assert.Equal(t, "updated", base.BaseHash.String())
	child, err := store.LookupBranch(ctx, "child")
	require.NoError(t, err)
	assert.Equal(t, "base", child.Base)

	tx = store.BeginBranchTx()
	require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{Name: "child", BaseHash: "prior"}))
	err = tx.UpsertMany(ctx, []state.UpsertRequest{
		{Name: "child", BaseHash: "retained"},
		{Name: "created-before-failure", Base: "main"},
		{Name: "base", Base: "child"},
		{Name: "after-failure", Base: "main"},
		{Name: "untracked", Base: "missing"},
		{Name: "dependent", Base: "untracked"},
		{Name: "without-base"},
	})
	require.ErrorContains(t, err, "would create a cycle")
	failed, ok := errors.AsType[*state.UpsertManyError](err)
	require.True(t, ok)
	assert.Equal(t, []int{2, 4, 5, 6}, failed.Indexes)
	require.Len(t, failed.Errors, 4)
	assert.ErrorContains(t, failed.Errors[0], "would create a cycle")
	assert.ErrorContains(t, failed.Errors[1], "branch missing is not tracked")
	assert.ErrorContains(t, failed.Errors[2], "branch untracked is not tracked")
	assert.ErrorContains(t, failed.Errors[3], "new branch must have a base")
	require.NoError(t, tx.Commit(ctx, "keep accepted requests"))
	child, err = store.LookupBranch(ctx, "child")
	require.NoError(t, err)
	assert.Equal(t, "retained", child.BaseHash.String())
	_, err = store.LookupBranch(ctx, "created-before-failure")
	assert.NoError(t, err)
	_, err = store.LookupBranch(ctx, "after-failure")
	assert.NoError(t, err)
}

func TestBranchTx_UpsertMany_operationalFailure(t *testing.T) {
	ctx := t.Context()
	mem := make(storage.MapBackend)
	store, err := state.InitStore(ctx, state.InitStoreRequest{DB: storage.NewDB(mem), Trunk: "main"})
	require.NoError(t, err)
	mem["branches/base"] = []byte(`{"base":{"name":"main","hash":"initial"}}`)
	mem["branches/tip"] = []byte(`{"base":{"name":"corrupt"}}`)
	mem["branches/corrupt"] = []byte(`{`)
	tx := store.BeginBranchTx()
	require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{Name: "base", BaseHash: "prior"}))
	err = tx.UpsertMany(ctx, []state.UpsertRequest{
		{Name: "base", BaseHash: "discarded"},
		{Name: "new", Base: "tip"},
	})
	require.Error(t, err)
	_, partial := errors.AsType[*state.UpsertManyError](err)
	assert.False(t, partial)
	require.NoError(t, tx.Commit(ctx, "keep earlier invocation"))
	base, err := store.LookupBranch(ctx, "base")
	require.NoError(t, err)
	assert.Equal(t, "prior", base.BaseHash.String())
	_, err = store.LookupBranch(ctx, "new")
	assert.ErrorIs(t, err, state.ErrNotExist)
}

func TestBranchTxUpsertChangeMetadataCanClear(t *testing.T) {
	ctx := t.Context()
	db := storage.NewDB(make(storage.MapBackend))
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    db,
		Trunk: "main",
	})
	require.NoError(t, err)

	require.NoError(t, statetest.UpdateBranch(ctx, store, &statetest.UpdateRequest{
		Upserts: []state.UpsertRequest{
			{
				Name:           "foo",
				Base:           "main",
				ChangeMetadata: jsontext.Value(`{"number": 123}`),
				ChangeForge:    "github",
			},
		},
		Message: "add foo",
	}))

	foo, err := store.LookupBranch(ctx, "foo")
	require.NoError(t, err)
	assert.Equal(t, "github", foo.ChangeForge)
	assert.JSONEq(t, `{"number": 123}`, string(foo.ChangeMetadata))

	require.NoError(t, statetest.UpdateBranch(ctx, store, &statetest.UpdateRequest{
		Upserts: []state.UpsertRequest{
			{
				Name:           "foo",
				Base:           "main",
				ChangeMetadata: state.Null,
			},
		},
		Message: "clear foo",
	}))

	foo, err = store.LookupBranch(ctx, "foo")
	require.NoError(t, err)
	assert.Equal(t, "", foo.ChangeForge)
	assert.Nil(t, foo.ChangeMetadata)
}

func TestBranchTxUpsert_canClearUpstream(t *testing.T) {
	ctx := t.Context()
	db := storage.NewDB(make(storage.MapBackend))
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    db,
		Trunk: "main",
	})
	require.NoError(t, err)

	upstream := "thing"
	require.NoError(t, statetest.UpdateBranch(ctx, store, &statetest.UpdateRequest{
		Upserts: []state.UpsertRequest{
			{
				Name:           "foo",
				Base:           "main",
				UpstreamBranch: &upstream,
			},
		},
		Message: "add foo",
	}))

	foo, err := store.LookupBranch(ctx, "foo")
	require.NoError(t, err)
	assert.Equal(t, "thing", foo.UpstreamBranch)

	var empty string
	require.NoError(t, statetest.UpdateBranch(ctx, store, &statetest.UpdateRequest{
		Upserts: []state.UpsertRequest{
			{
				Name:           "foo",
				Base:           "main",
				UpstreamBranch: &empty,
			},
		},
		Message: "clear foo",
	}))

	foo, err = store.LookupBranch(ctx, "foo")
	require.NoError(t, err)
	assert.Equal(t, "", foo.UpstreamBranch)
}

func TestStore_LookupBranches(t *testing.T) {
	ctx := t.Context()
	db := &getManyDB{
		DB: storage.NewDB(make(storage.MapBackend)),
	}
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    db,
		Trunk: "main",
	})
	require.NoError(t, err)

	upstream := "origin/foo"
	require.NoError(t, statetest.UpdateBranch(ctx, store, &statetest.UpdateRequest{
		Upserts: []state.UpsertRequest{
			{
				Name:           "foo",
				Base:           "main",
				BaseHash:       "abc123",
				ChangeMetadata: jsontext.Value(`{"number": 123}`),
				ChangeForge:    "github",
				UpstreamBranch: &upstream,
			},
			{
				Name: "bar",
				Base: "foo",
			},
		},
		Message: "add branches",
	}))

	db.getManyCalls = 0
	branches, err := store.LookupBranches(ctx, []string{"foo", "bar"})
	require.NoError(t, err)

	require.Len(t, branches, 2)
	assert.Equal(t, "main", branches[0].Base)
	assert.Equal(t, "abc123", branches[0].BaseHash.String())
	assert.Equal(t, "github", branches[0].ChangeForge)
	assert.JSONEq(t, `{"number": 123}`, string(branches[0].ChangeMetadata))
	assert.Equal(t, "origin/foo", branches[0].UpstreamBranch)
	assert.Equal(t, "foo", branches[1].Base)
	assert.Equal(t, []string{"branches/foo", "branches/bar"}, db.getManyKeys)
	assert.Equal(t, 1, db.getManyCalls)
}

func TestStore_LookupBranch_usesGetMany(t *testing.T) {
	ctx := t.Context()
	db := &getManyDB{DB: storage.NewDB(make(storage.MapBackend))}
	store, err := state.InitStore(ctx, state.InitStoreRequest{DB: db, Trunk: "main"})
	require.NoError(t, err)
	require.NoError(t, statetest.UpdateBranch(ctx, store, &statetest.UpdateRequest{
		Upserts: []state.UpsertRequest{{Name: "feature", Base: "main"}},
	}))
	db.getManyCalls = 0
	_, err = store.LookupBranch(ctx, "feature")
	require.NoError(t, err)
	assert.Equal(t, 1, db.getManyCalls)
	assert.Equal(t, []string{"branches/feature"}, db.getManyKeys)
}

func TestStore_LookupBranches_missing(t *testing.T) {
	ctx := t.Context()
	db := &getManyDB{DB: storage.NewDB(make(storage.MapBackend))}
	store, err := state.InitStore(ctx, state.InitStoreRequest{DB: db, Trunk: "main"})
	require.NoError(t, err)
	require.NoError(t, statetest.UpdateBranch(ctx, store, &statetest.UpdateRequest{
		Upserts: []state.UpsertRequest{{Name: "feature", Base: "main", BaseHash: "abc123"}},
	}))
	db.getManyCalls = 0

	responses, err := store.LookupBranches(ctx, []string{"missing", "feature", "missing", "feature"})
	missing, ok := errors.AsType[*storage.MissingKeysError](err)
	require.True(t, ok, "expected missing keys, got %v", err)
	assert.Equal(t, []int{0, 2}, missing.Indexes)
	assert.Equal(t, []string{"branches/missing", "branches/missing"}, missing.Keys)
	require.Len(t, responses, 4)
	assert.Nil(t, responses[0])
	assert.Equal(t, "main", responses[1].Base)
	assert.Nil(t, responses[2])
	assert.Equal(t, responses[1], responses[3])
	assert.Equal(t, 1, db.getManyCalls)
}

// Uses rapid to run randomized scenarios on the branch state
// to ensure we never leave it in a corrupted state.
func TestBranchStateUncorruptible(t *testing.T) {
	rapid.Check(t, testBranchStateUncorruptible)
}

func FuzzBranchStateUncorruptible(f *testing.F) {
	f.Fuzz(rapid.MakeFuzz(testBranchStateUncorruptible))
}

func testBranchStateUncorruptible(t *rapid.T) {
	branchNameRune := rapid.RuneFrom([]rune("abcdefghijklmnopqrstuvwxyz"))
	branchNameGen := rapid.StringOfN(branchNameRune, 1, 2, -1)
	// 26 * 26 gives us plenty of branches to work with.

	trunk := branchNameGen.Draw(t, "trunk")
	ctx := t.Context()
	db := storage.NewDB(make(storage.MapBackend))
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    db,
		Trunk: trunk,
	})
	require.NoError(t, err)

	sm := &branchStateUncorruptible{
		ctx:           ctx,
		db:            db,
		store:         store,
		trunk:         trunk,
		knownBranches: make(map[string]struct{}),
	}
	sm.branchNameGen = branchNameGen.Filter(func(name string) bool { return name != sm.trunk })

	sm.knownBranchGen = rapid.Custom(func(t *rapid.T) string {
		if len(sm.knownBranches) == 0 {
			return rapid.Just(sm.trunk).Draw(t, "trunk")
		}
		return rapid.SampledFrom(
			slices.Collect(maps.Keys(sm.knownBranches)),
		).Draw(t, "knownBranch")
	})

	t.Repeat(rapid.StateMachineActions(sm))
}

type branchStateUncorruptible struct {
	ctx context.Context

	db    state.DB
	store *state.Store
	trunk string

	knownBranches map[string]struct{}

	branchNameGen  *rapid.Generator[string]
	knownBranchGen *rapid.Generator[string]
}

func (sm *branchStateUncorruptible) Check(t *rapid.T) {
	// Listed listedBranches must always match knownBranches.
	knownBranches := slices.Sorted(maps.Keys(sm.knownBranches))
	listedBranches, err := sliceutil.CollectErr(sm.store.ListBranches(sm.ctx))
	require.NoError(t, err)
	slices.Sort(listedBranches)
	if len(listedBranches) == 0 {
		listedBranches = nil // knownBranches is nil for empty
	}
	assert.Equal(t, knownBranches, listedBranches,
		"known branches does not match listed branches")

	// Trunk must never be tracked.
	_, err = sm.store.LookupBranch(sm.ctx, sm.trunk)
	assert.ErrorIs(t, err, state.ErrNotExist)

	// Verify that there are no cycles in the branch graph
	// and that all bases are either the trunk, or tracked.
	//
	// This is pretty inefficient since it'll do repeated work
	// for the same branches, but it's fine for now.
	for branchName := range sm.knownBranches {
		sm.checkBranch(t, branchName)
	}
}

func (sm *branchStateUncorruptible) checkBranch(t *rapid.T, name string) {
	seen := make(map[string]struct{})
	var path []string
	for name != sm.trunk {
		b, err := sm.store.LookupBranch(sm.ctx, name)
		require.NoError(t, err, "lookup branch %q (path: %v)", name, append(path, name))

		if _, ok := seen[name]; ok {
			t.Fatalf("cycle detected: %v", append(path, name))
		}

		seen[name] = struct{}{}
		path = append(path, name)
		name = b.Base
	}
}

func (sm *branchStateUncorruptible) update(t *rapid.T, req *statetest.UpdateRequest) {
	t.Logf("try update: %v", req.Message)
	if err := statetest.UpdateBranch(sm.ctx, sm.store, req); err != nil {
		for _, upsert := range req.Upserts {
			t.Logf("failed to upsert branch: name=%v base=%v: %v", upsert.Name, upsert.Base, err)
		}
		for _, del := range req.Deletes {
			t.Logf("failed to delete branch: name=%v: %v", del, err)
		}

		return
	}

	for _, upsert := range req.Upserts {
		sm.knownBranches[upsert.Name] = struct{}{}
		t.Logf("upsert branch: name=%v base=%v", upsert.Name, upsert.Base)
	}

	for _, del := range req.Deletes {
		delete(sm.knownBranches, del)
		t.Logf("delete branch: name=%v", del)
	}
}

func (sm *branchStateUncorruptible) DeleteOne(t *rapid.T) {
	sm.update(t, &statetest.UpdateRequest{
		Deletes: []string{
			sm.branchNameGen.Draw(t, "branchToDelete"),
		},
		Message: "delete random branch",
	})
}

func (sm *branchStateUncorruptible) DeleteOneTx(t *rapid.T) {
	name := sm.branchNameGen.Draw(t, "branchToDelete")
	tx := sm.store.BeginBranchTx()

	if err := tx.Delete(sm.ctx, name); err != nil {
		t.Logf("failed to delete branch %q: %v", name, err)
	} else {
		t.Logf("deleted branch %q", name)
		delete(sm.knownBranches, name)
	}

	require.NoError(t, tx.Commit(sm.ctx, "delete branch"))
}

func (sm *branchStateUncorruptible) UpsertOne(t *rapid.T) {
	sm.update(t, &statetest.UpdateRequest{
		Upserts: []state.UpsertRequest{
			{
				Name: sm.branchNameGen.Draw(t, "branch"),
				Base: sm.branchNameGen.Draw(t, "base"),
			},
		},
		Message: "upsert random branch",
	})
}

func (sm *branchStateUncorruptible) UpsertOneTx(t *rapid.T) {
	name := sm.branchNameGen.Draw(t, "branch")
	base := sm.branchNameGen.Draw(t, "base")
	tx := sm.store.BeginBranchTx()

	if err := tx.Upsert(sm.ctx, state.UpsertRequest{
		Name: name,
		Base: base,
	}); err != nil {
		t.Logf("failed to upsert branch %q: %v", name, err)
	} else {
		t.Logf("upserted branch %q with base %q", name, base)
		sm.knownBranches[name] = struct{}{}
	}

	require.NoError(t, tx.Commit(sm.ctx, "upsert branch"))
}

func (sm *branchStateUncorruptible) UpsertAndDeleteMany(t *rapid.T) {
	sm.update(t, &statetest.UpdateRequest{
		Upserts: rapid.SliceOf(rapid.Custom(func(t *rapid.T) state.UpsertRequest {
			return state.UpsertRequest{
				Name: sm.branchNameGen.Draw(t, "branch"),
				Base: sm.branchNameGen.Draw(t, "base"),
			}
		})).Draw(t, "upserts"),
		Deletes: rapid.SliceOf(sm.branchNameGen).Draw(t, "deletes"),
		Message: "upsert and delete random branches",
	})
}

func (sm *branchStateUncorruptible) UpsertAndDeleteManyTx(t *rapid.T) {
	operationGen := rapid.SampledFrom([]string{"upsert", "delete"})

	upsertGen := rapid.Custom(func(t *rapid.T) state.UpsertRequest {
		return state.UpsertRequest{
			Name: sm.branchNameGen.Draw(t, "branch"),
			Base: sm.branchNameGen.Draw(t, "base"),
		}
	})

	tx := sm.store.BeginBranchTx()
	for range rapid.IntRange(0, 10).Draw(t, "operations") {
		switch operationGen.Draw(t, "operation") {
		case "upsert":
			req := upsertGen.Draw(t, "upsert")
			if err := tx.Upsert(sm.ctx, req); err != nil {
				t.Logf("failed to upsert branch %q: %v", req.Name, err)
			} else {
				t.Logf("upserted branch %q with base %q", req.Name, req.Base)
				sm.knownBranches[req.Name] = struct{}{}
			}

		case "delete":
			name := sm.branchNameGen.Draw(t, "branchToDelete")
			if err := tx.Delete(sm.ctx, name); err != nil {
				t.Logf("failed to delete branch %q: %v", name, err)
			} else {
				t.Logf("deleted branch %q", name)
				delete(sm.knownBranches, name)
			}

		default:
			t.Fatalf("unexpected operation")
		}
	}

	require.NoError(t, tx.Commit(sm.ctx, "upsert and delete branches"))
}

func (sm *branchStateUncorruptible) UpsertAlwaysSuccess(t *rapid.T) {
	newBranchGen := sm.branchNameGen.Filter(func(name string) bool {
		_, ok := sm.knownBranches[name]
		return !ok
	})

	sm.update(t, &statetest.UpdateRequest{
		Upserts: []state.UpsertRequest{
			{
				Name: newBranchGen.Draw(t, "branch"),
				Base: sm.knownBranchGen.Draw(t, "base"),
			},
		},
		Message: "upsert branch with known base",
	})
}

func (sm *branchStateUncorruptible) ChangeTrunkToKnownBranchFails(t *rapid.T) {
	newTrunk := sm.knownBranchGen.Draw(t, "newTrunk")
	if newTrunk == sm.trunk {
		t.Skip("new trunk is the same as the current trunk")
	}

	_, err := state.InitStore(sm.ctx, state.InitStoreRequest{
		DB:    sm.db,
		Trunk: newTrunk,
	})
	assert.Error(t, err, "unexpectedly succeeded in changing trunk to %q", newTrunk)
}

func (sm *branchStateUncorruptible) ChangeTrunk(t *rapid.T) {
	newTrunk := sm.branchNameGen.Filter(func(name string) bool {
		return name != sm.trunk
	}).Draw(t, "newTrunk")

	store, err := state.InitStore(sm.ctx, state.InitStoreRequest{
		DB:    sm.db,
		Trunk: newTrunk,
	})
	if err != nil {
		t.Logf("failed to change trunk to %q: %v", newTrunk, err)
		return
	}

	sm.store = store
	sm.trunk = newTrunk
	t.Logf("changed trunk to %q", newTrunk)
}

type getManyDB struct {
	*storage.DB

	getManyCalls int
	getManyKeys  []string
}

func (db *getManyDB) GetMany(
	ctx context.Context,
	reqs []storage.GetRequest,
) error {
	db.getManyCalls++
	db.getManyKeys = make([]string, 0, len(reqs))
	for _, req := range reqs {
		db.getManyKeys = append(db.getManyKeys, req.Key)
	}
	return db.DB.GetMany(ctx, reqs)
}
