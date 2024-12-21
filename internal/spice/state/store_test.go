package state_test

import (
	"context"
	"encoding/json"
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/logtest"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

func TestStore(t *testing.T) {
	ctx := context.Background()
	db := storage.NewDB(storage.NewMemBackend())

	_, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    db,
		Trunk: "main",
	})
	require.NoError(t, err)

	store, err := state.OpenStore(ctx, db, logtest.New(t))
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
		mem := storage.NewMemBackend()
		mem.AddFiles(maps.All(map[string][]byte{
			"version": []byte("500"),
		}))

		_, err := state.OpenStore(context.Background(), storage.NewDB(mem), nil)
		require.Error(t, err)
		assert.ErrorContains(t, err, "check store layout:")
		assert.ErrorAs(t, err, new(*state.VersionMismatchError))
	})

	t.Run("NotInitialized", func(t *testing.T) {
		mem := storage.NewMemBackend()
		_, err := state.OpenStore(context.Background(), storage.NewDB(mem), nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, state.ErrUninitialized)
	})

	t.Run("CorruptRepo/Unparseable", func(t *testing.T) {
		mem := storage.NewMemBackend()
		mem.AddFiles(maps.All(map[string][]byte{
			"repo": []byte(`{`),
		}))

		_, err := state.OpenStore(context.Background(), storage.NewDB(mem), nil)
		require.Error(t, err)
		assert.ErrorContains(t, err, "get repo state:")
	})

	t.Run("CorruptRepo/Incomplete", func(t *testing.T) {
		mem := storage.NewMemBackend()
		mem.AddFiles(maps.All(map[string][]byte{
			"repo": []byte(`{}`),
		}))

		_, err := state.OpenStore(context.Background(), storage.NewDB(mem), nil)
		require.Error(t, err)
		assert.ErrorContains(t, err, "corrupt state:")
	})
}
