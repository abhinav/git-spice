package state_test

import (
	"context"
	"encoding/json"
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
		_, err := store.LookupBranch(ctx, "main")
		assert.ErrorIs(t, err, state.ErrNotExist)
	})

	err = store.UpdateBranch(ctx, &state.UpdateRequest{
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

	t.Run("overwrite", func(t *testing.T) {
		err := store.UpdateBranch(ctx, &state.UpdateRequest{
			Upserts: []state.UpsertRequest{{
				Name:           "foo",
				Base:           "bar",
				BaseHash:       "54321",
				ChangeForge:    "shamhub",
				ChangeMetadata: json.RawMessage(`{"id": 43}`),
			}},
		})
		require.NoError(t, err)

		res, err := store.LookupBranch(ctx, "foo")
		require.NoError(t, err)

		assert.Equal(t, "bar", res.Base)
		assert.Equal(t, "54321", string(res.BaseHash))
		assert.Equal(t, "shamhub", res.ChangeForge)
		assert.JSONEq(t, `{"id": 43}`, string(res.ChangeMetadata))
	})

	t.Run("name with slash", func(t *testing.T) {
		err := store.UpdateBranch(ctx, &state.UpdateRequest{
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
}
