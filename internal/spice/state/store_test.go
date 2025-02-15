package state_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/logutil"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

func TestStore(t *testing.T) {
	ctx := t.Context()
	db := storage.NewDB(make(storage.MapBackend))

	_, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    db,
		Trunk: "main",
	})
	require.NoError(t, err)

	store, err := state.OpenStore(ctx, db, logutil.TestLogger(t))
	require.NoError(t, err)

	t.Run("empty", func(t *testing.T) {
		_, err := store.LookupBranch(ctx, "foo")
		assert.ErrorIs(t, err, state.ErrNotExist)
	})

	err = state.UpdateBranch(ctx, store, &state.UpdateRequest{
		Upserts: []state.UpsertRequest{{
			Name:           "foo",
			Base:           "main",
			BaseHash:       "123456",
			ChangeForge:    "shamhub",
			ChangeMetadata: json.RawMessage(`{"number": 42}`),
		}},
	})
	require.NoError(t, err)

	t.Run("get", func(t *testing.T) {
		ctx := t.Context()
		res, err := store.LookupBranch(ctx, "foo")
		require.NoError(t, err)

		assert.Equal(t, "main", res.Base)
		assert.Equal(t, "123456", string(res.BaseHash))
		assert.Equal(t, "shamhub", res.ChangeForge)
		assert.JSONEq(t, `{"number": 42}`, string(res.ChangeMetadata))
	})

	require.NoError(t, state.UpdateBranch(ctx, store, &state.UpdateRequest{
		Upserts: []state.UpsertRequest{{
			Name:           "bar2",
			Base:           "main",
			BaseHash:       "abcdef",
			ChangeForge:    "shamhub",
			ChangeMetadata: json.RawMessage(`{"id": 42}`),
		}},
	}))

	t.Run("overwrite", func(t *testing.T) {
		ctx := t.Context()
		err := state.UpdateBranch(ctx, store, &state.UpdateRequest{
			Upserts: []state.UpsertRequest{
				{
					Name:           "foo",
					Base:           "bar2",
					BaseHash:       "54321",
					ChangeForge:    "shamhub",
					ChangeMetadata: json.RawMessage(`{"id": 43}`),
				},
			},
		})
		require.NoError(t, err)

		res, err := store.LookupBranch(ctx, "foo")
		require.NoError(t, err)

		assert.Equal(t, "bar2", res.Base)
		assert.Equal(t, "54321", string(res.BaseHash))
		assert.Equal(t, "shamhub", res.ChangeForge)
		assert.JSONEq(t, `{"id": 43}`, string(res.ChangeMetadata))
	})

	t.Run("name with slash", func(t *testing.T) {
		ctx := t.Context()
		err := state.UpdateBranch(ctx, store, &state.UpdateRequest{
			Upserts: []state.UpsertRequest{{
				Name:           "bar/baz",
				Base:           "main",
				ChangeForge:    "shamhub",
				ChangeMetadata: json.RawMessage(`{"id": 44}`),
				BaseHash:       "abcdef",
			}},
		})
		require.NoError(t, err)

		res, err := store.LookupBranch(ctx, "bar/baz")
		require.NoError(t, err)
		assert.Equal(t, "main", res.Base)
		assert.Equal(t, "abcdef", string(res.BaseHash))
		assert.Equal(t, "shamhub", res.ChangeForge)
		assert.JSONEq(t, `{"id": 44}`, string(res.ChangeMetadata))
	})

	t.Run("upstream branch", func(t *testing.T) {
		ctx := t.Context()
		upstream := "remoteBranch"
		err := state.UpdateBranch(ctx, store, &state.UpdateRequest{
			Upserts: []state.UpsertRequest{{
				Name:           "localBranch",
				Base:           "main",
				BaseHash:       "abcdef",
				UpstreamBranch: &upstream,
			}},
		})
		require.NoError(t, err)

		res, err := store.LookupBranch(ctx, "localBranch")
		require.NoError(t, err)

		assert.Equal(t, "main", res.Base)
		assert.Equal(t, "abcdef", string(res.BaseHash))
		assert.Equal(t, "remoteBranch", res.UpstreamBranch)
	})
}

func TestOpenStore_errors(t *testing.T) {
	t.Run("VersionMismatch", func(t *testing.T) {
		mem := storage.MapBackend{
			"version": []byte("500"),
		}

		_, err := state.OpenStore(t.Context(), storage.NewDB(mem), nil)
		require.Error(t, err)
		assert.ErrorContains(t, err, "check store layout:")
		assert.ErrorAs(t, err, new(*state.VersionMismatchError))
	})

	t.Run("NotInitialized", func(t *testing.T) {
		mem := make(storage.MapBackend)
		_, err := state.OpenStore(t.Context(), storage.NewDB(mem), nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, state.ErrUninitialized)
	})

	t.Run("CorruptRepo/Unparseable", func(t *testing.T) {
		mem := storage.MapBackend{
			"repo": []byte(`{`),
		}

		_, err := state.OpenStore(t.Context(), storage.NewDB(mem), nil)
		require.Error(t, err)
		assert.ErrorContains(t, err, "get repo state:")
	})

	t.Run("CorruptRepo/Incomplete", func(t *testing.T) {
		mem := storage.MapBackend{
			"repo": []byte(`{}`),
		}

		_, err := state.OpenStore(t.Context(), storage.NewDB(mem), nil)
		require.Error(t, err)
		assert.ErrorContains(t, err, "corrupt state:")
	})
}
