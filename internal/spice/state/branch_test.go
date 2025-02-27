package state

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/spice/state/storage"
	"pgregory.net/rapid"
)

func TestBranchChangeStateUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		give string

		want    *branchChangeState
		wantErr string
	}{
		{
			name: "Valid",
			give: `{"github": {"number": 123}}`,
			want: &branchChangeState{
				Forge:  "github",
				Change: json.RawMessage(`{"number": 123}`),
			},
		},
		{
			name:    "NotAnObject",
			give:    `123`,
			wantErr: "unmarshal change state",
		},
		{
			name: "MultipleForges",
			give: `{
				"github": {"number": 123},
				"gitlab": {"number": 456}
			}`,
			wantErr: "expected 1 forge key, got 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got branchChangeState
			err := json.Unmarshal([]byte(tt.give), &got)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, &got)
		})
	}
}

func TestBranchStateUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		give string

		want    *branchState
		wantErr string
	}{
		{
			name: "Simple",
			give: `{
				"base": {"name": "main", "hash": "abc123"},
				"upstream": {"branch": "main"},
				"change": {"github": {"number": 123}}
			}`,
			want: &branchState{
				Base: branchStateBase{
					Name: "main",
					Hash: "abc123",
				},
				Upstream: &branchUpstreamState{
					Branch: "main",
				},
				Change: &branchChangeState{
					Forge:  "github",
					Change: json.RawMessage(`{"number": 123}`),
				},
			},
		},

		{
			name: "NoUpstream",
			give: `{
				"base": {"name": "main", "hash": "abc123"}
			}`,
			want: &branchState{
				Base: branchStateBase{
					Name: "main",
					Hash: "abc123",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got branchState
			err := json.Unmarshal([]byte(tt.give), &got)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, &got)
		})
	}
}

func TestBranchTxUpsertErrors(t *testing.T) {
	ctx := t.Context()
	db := storage.NewDB(make(storage.MapBackend))
	store, err := InitStore(ctx, InitStoreRequest{
		DB:    db,
		Trunk: "main",
	})
	require.NoError(t, err)

	t.Run("MissingBranch", func(t *testing.T) {
		ctx := t.Context()
		tx := store.BeginBranchTx()
		err := tx.Upsert(ctx, UpsertRequest{})
		assert.ErrorContains(t, err, "branch name is required")
		require.NoError(t, tx.Commit(ctx, "no op"))
	})

	t.Run("TrunkNotAllowed", func(t *testing.T) {
		ctx := t.Context()
		tx := store.BeginBranchTx()
		err := tx.Upsert(ctx, UpsertRequest{
			Name: "main",
			Base: "whatever",
		})
		assert.ErrorIs(t, err, ErrTrunk)
		require.NoError(t, tx.Commit(ctx, "no op"))

		_, err = store.LookupBranch(ctx, "main")
		require.ErrorIs(t, err, ErrNotExist)
	})

	t.Run("NewBranchNoBase", func(t *testing.T) {
		ctx := t.Context()
		tx := store.BeginBranchTx()
		err := tx.Upsert(ctx, UpsertRequest{
			Name: "foo",
		})
		assert.ErrorContains(t, err, "new branch must have a base")
		require.NoError(t, tx.Commit(ctx, "no op"))

		_, err = store.LookupBranch(ctx, "foo")
		require.ErrorIs(t, err, ErrNotExist)
	})

	t.Run("NewBranchUnknownBase", func(t *testing.T) {
		ctx := t.Context()
		tx := store.BeginBranchTx()
		err := tx.Upsert(ctx, UpsertRequest{
			Name: "foo",
			Base: "unknown",
		})
		assert.ErrorContains(t, err, "branch unknown is not tracked")
		require.NoError(t, tx.Commit(ctx, "no op"))

		_, err = store.LookupBranch(ctx, "foo")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrNotExist)
	})

	// Add a couple branches to work with.
	{
		tx := store.BeginBranchTx()
		require.NoError(t, tx.Upsert(ctx, UpsertRequest{
			Name: "foo",
			Base: "main",
		}))
		require.NoError(t, tx.Upsert(ctx, UpsertRequest{
			Name: "bar",
			Base: "foo",
		}))
		require.NoError(t, tx.Commit(ctx, "add foo and bar"))
	}

	t.Run("Cycle", func(t *testing.T) {
		ctx := t.Context()
		tx := store.BeginBranchTx()
		err := tx.Upsert(ctx, UpsertRequest{
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
			require.NoError(t, tx.Upsert(ctx, UpsertRequest{
				Name: "baz",
				Base: "main",
			}))
			require.NoError(t, tx.Commit(ctx, "add baz"))
		}

		tx := store.BeginBranchTx()
		require.NoError(t, tx.Delete(ctx, "baz"))
		err := tx.Upsert(ctx, UpsertRequest{
			Name: "qux",
			Base: "baz",
		})
		assert.ErrorContains(t, err, `branch baz is not tracked`)
		require.NoError(t, tx.Commit(ctx, "delete baz"))

		_, err = store.LookupBranch(ctx, "baz")
		require.ErrorIs(t, err, ErrNotExist)
	})
}

func TestBranchTxDelete(t *testing.T) {
	ctx := t.Context()
	db := storage.NewDB(make(storage.MapBackend))
	store, err := InitStore(ctx, InitStoreRequest{
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
		assert.ErrorIs(t, err, ErrTrunk)
		require.NoError(t, tx.Commit(ctx, "no op"))
	})

	t.Run("UnknownBranch", func(t *testing.T) {
		ctx := t.Context()
		tx := store.BeginBranchTx()
		err := tx.Delete(ctx, "unknown")
		assert.ErrorIs(t, err, ErrNotExist)
	})

	// Add a couple branches to work with.
	{
		tx := store.BeginBranchTx()
		require.NoError(t, tx.Upsert(ctx, UpsertRequest{
			Name: "foo",
			Base: "main",
		}))
		require.NoError(t, tx.Upsert(ctx, UpsertRequest{
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
		require.NoError(t, tx.Upsert(ctx, UpsertRequest{
			Name: "baz",
			Base: "main",
		}))
		require.NoError(t, tx.Delete(ctx, "baz"))
		require.NoError(t, tx.Commit(ctx, "no op"))

		_, err := store.LookupBranch(ctx, "baz")
		require.ErrorIs(t, err, ErrNotExist)
	})
}

func TestBranchTxUpsertChangeMetadataCanClear(t *testing.T) {
	ctx := t.Context()
	db := storage.NewDB(make(storage.MapBackend))
	store, err := InitStore(ctx, InitStoreRequest{
		DB:    db,
		Trunk: "main",
	})
	require.NoError(t, err)

	require.NoError(t, UpdateBranch(ctx, store, &UpdateRequest{
		Upserts: []UpsertRequest{
			{
				Name:           "foo",
				Base:           "main",
				ChangeMetadata: json.RawMessage(`{"number": 123}`),
				ChangeForge:    "github",
			},
		},
		Message: "add foo",
	}))

	foo, err := store.LookupBranch(ctx, "foo")
	require.NoError(t, err)
	assert.Equal(t, "github", foo.ChangeForge)
	assert.JSONEq(t, `{"number": 123}`, string(foo.ChangeMetadata))

	require.NoError(t, UpdateBranch(ctx, store, &UpdateRequest{
		Upserts: []UpsertRequest{
			{
				Name:           "foo",
				Base:           "main",
				ChangeMetadata: Null,
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
	store, err := InitStore(ctx, InitStoreRequest{
		DB:    db,
		Trunk: "main",
	})
	require.NoError(t, err)

	upstream := "thing"
	require.NoError(t, UpdateBranch(ctx, store, &UpdateRequest{
		Upserts: []UpsertRequest{
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
	require.NoError(t, UpdateBranch(ctx, store, &UpdateRequest{
		Upserts: []UpsertRequest{
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
	store, err := InitStore(ctx, InitStoreRequest{
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
		branchNameGen: branchNameGen,
	}

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

	db    DB
	store *Store
	trunk string

	knownBranches map[string]struct{}

	branchNameGen  *rapid.Generator[string]
	knownBranchGen *rapid.Generator[string]
}

func (sm *branchStateUncorruptible) Check(t *rapid.T) {
	// Listed listedBranches must always match knownBranches.
	knownBranches := slices.Sorted(maps.Keys(sm.knownBranches))
	listedBranches, err := sm.store.ListBranches(sm.ctx)
	require.NoError(t, err)
	slices.Sort(listedBranches)
	if len(listedBranches) == 0 {
		listedBranches = nil // knownBranches is nil for empty
	}
	assert.Equal(t, knownBranches, listedBranches,
		"known branches does not match listed branches")

	// Trunk must never be tracked.
	_, err = sm.store.LookupBranch(sm.ctx, sm.trunk)
	assert.ErrorIs(t, err, ErrNotExist)

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

func (sm *branchStateUncorruptible) update(t *rapid.T, req *UpdateRequest) {
	t.Logf("try update: %v", req.Message)
	if err := UpdateBranch(sm.ctx, sm.store, req); err != nil {
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
	sm.update(t, &UpdateRequest{
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
	sm.update(t, &UpdateRequest{
		Upserts: []UpsertRequest{
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

	if err := tx.Upsert(sm.ctx, UpsertRequest{
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
	sm.update(t, &UpdateRequest{
		Upserts: rapid.SliceOf(rapid.Custom(func(t *rapid.T) UpsertRequest {
			return UpsertRequest{
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

	upsertGen := rapid.Custom(func(t *rapid.T) UpsertRequest {
		return UpsertRequest{
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

	sm.update(t, &UpdateRequest{
		Upserts: []UpsertRequest{
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

	_, err := InitStore(sm.ctx, InitStoreRequest{
		DB:    sm.db,
		Trunk: newTrunk,
	})
	assert.Error(t, err, "unexpectedly succeeded in changing trunk to %q", newTrunk)
}

func (sm *branchStateUncorruptible) ChangeTrunk(t *rapid.T) {
	newTrunk := sm.branchNameGen.Filter(func(name string) bool {
		return name != sm.trunk
	}).Draw(t, "newTrunk")

	store, err := InitStore(sm.ctx, InitStoreRequest{
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

// UpdateRequest is a request to add, update, or delete information about branches.
type UpdateRequest struct {
	// Upserts are requests to add or update information about branches.
	Upserts []UpsertRequest

	// Deletes are requests to delete information about branches.
	Deletes []string

	// Message is a message specifying the reason for the update.
	// This will be persisted in the Git commit message.
	Message string
}

func UpdateBranch(ctx context.Context, s *Store, req *UpdateRequest) error {
	tx := s.BeginBranchTx()
	for idx, upsert := range req.Upserts {
		if err := tx.Upsert(ctx, upsert); err != nil {
			return fmt.Errorf("upsert [%d] %q: %w", idx, upsert.Name, err)
		}
	}

	for idx, name := range req.Deletes {
		if err := tx.Delete(ctx, name); err != nil {
			return fmt.Errorf("delete [%d] %q: %w", idx, name, err)
		}
	}

	if err := tx.Commit(ctx, req.Message); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}
