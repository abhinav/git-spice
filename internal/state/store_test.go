package state_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/logtest"
	"go.abhg.dev/gs/internal/state"
)

func TestIntegrationStore(t *testing.T) {
	repoDir := t.TempDir()
	ctx := context.Background()
	repo, err := git.Init(ctx, repoDir, git.InitOptions{
		Log:    logtest.New(t),
		Branch: "main",
	})
	require.NoError(t, err)

	t.Run("init", func(t *testing.T) {
		_, err := state.InitStore(ctx, state.InitStoreRequest{
			Repository: repo,
			Trunk:      "main",
		})
		require.NoError(t, err)
	})

	store, err := state.OpenStore(ctx, repo)
	require.NoError(t, err)

	t.Run("empty", func(t *testing.T) {
		_, err := store.GetBranch(ctx, "main")
		assert.ErrorIs(t, err, state.ErrNotExist)
	})

	err = store.SetBranch(ctx, state.SetBranchRequest{
		Name: "foo",
		Base: "main",
		PR:   42,
	})
	require.NoError(t, err)

	t.Run("get", func(t *testing.T) {
		res, err := store.GetBranch(ctx, "foo")
		require.NoError(t, err)

		assert.Equal(t, "main", res.Base)
		assert.Equal(t, 42, res.PR)
	})

	t.Run("overwrite", func(t *testing.T) {
		err := store.SetBranch(ctx, state.SetBranchRequest{
			Name: "foo",
			Base: "bar",
			PR:   43,
		})
		require.NoError(t, err)

		res, err := store.GetBranch(ctx, "foo")
		require.NoError(t, err)

		assert.Equal(t, "bar", res.Base)
		assert.Equal(t, 43, res.PR)
	})

	t.Run("name with slash", func(t *testing.T) {
		err := store.SetBranch(ctx, state.SetBranchRequest{
			Name: "bar/baz",
			Base: "main",
			PR:   44,
		})
		require.NoError(t, err)

		res, err := store.GetBranch(ctx, "bar/baz")
		require.NoError(t, err)
		assert.Equal(t, "main", res.Base)
		assert.Equal(t, 44, res.PR)
	})
}
