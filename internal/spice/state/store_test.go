package state_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/logtest"
	"go.abhg.dev/gs/internal/spice/state"
)

func TestIntegrationStore(t *testing.T) {
	repoDir := t.TempDir()
	ctx := context.Background()
	repo, err := git.Init(ctx, repoDir, git.InitOptions{
		Log:    logtest.New(t),
		Branch: "main",
	})
	require.NoError(t, err)

	_, err = state.InitStore(ctx, state.InitStoreRequest{
		Repository: repo,
		Trunk:      "main",
	})
	require.NoError(t, err)

	store, err := state.OpenStore(ctx, repo, logtest.New(t))
	require.NoError(t, err)

	t.Run("empty", func(t *testing.T) {
		_, err := store.LookupBranch(ctx, "main")
		assert.ErrorIs(t, err, state.ErrNotExist)
	})

	err = store.UpdateBranch(ctx, &state.UpdateRequest{
		Upserts: []state.UpsertRequest{{
			Name:     "foo",
			Base:     "main",
			BaseHash: "123456",
			PR:       42,
		}},
	})
	require.NoError(t, err)

	t.Run("get", func(t *testing.T) {
		res, err := store.LookupBranch(ctx, "foo")
		require.NoError(t, err)

		assert.Equal(t, &state.LookupResponse{
			Base:     "main",
			BaseHash: "123456",
			PR:       42,
		}, res)
	})

	t.Run("overwrite", func(t *testing.T) {
		err := store.UpdateBranch(ctx, &state.UpdateRequest{
			Upserts: []state.UpsertRequest{{
				Name:     "foo",
				Base:     "bar",
				BaseHash: "54321",
				PR:       43,
			}},
		})
		require.NoError(t, err)

		res, err := store.LookupBranch(ctx, "foo")
		require.NoError(t, err)

		assert.Equal(t, &state.LookupResponse{
			Base:     "bar",
			BaseHash: "54321",
			PR:       43,
		}, res)
	})

	t.Run("name with slash", func(t *testing.T) {
		err := store.UpdateBranch(ctx, &state.UpdateRequest{
			Upserts: []state.UpsertRequest{{
				Name:     "bar/baz",
				Base:     "main",
				PR:       44,
				BaseHash: "abcdef",
			}},
		})
		require.NoError(t, err)

		res, err := store.LookupBranch(ctx, "bar/baz")
		require.NoError(t, err)
		assert.Equal(t, &state.LookupResponse{
			Base:     "main",
			BaseHash: "abcdef",
			PR:       44,
		}, res)
	})
}
