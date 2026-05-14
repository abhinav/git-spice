package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

func TestStagedComments(t *testing.T) {
	ctx := t.Context()
	db := storage.NewDB(make(storage.MapBackend))

	_, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    db,
		Trunk: "main",
	})
	require.NoError(t, err)

	store, err := state.OpenStore(ctx, db, silogtest.New(t))
	require.NoError(t, err)

	t.Run("LoadEmpty", func(t *testing.T) {
		got, err := store.LoadStagedComments(ctx, "feat")
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("SaveAndLoad", func(t *testing.T) {
		comments := &state.StagedComments{
			NextID: 3,
			Comments: []state.StagedComment{
				{
					ID:   1,
					File: "main.go",
					Line: 42,
					Body: "Consider using a const here.",
				},
				{
					ID:       2,
					File:     "handler.go",
					Line:     15,
					Body:     "I agree with your suggestion.",
					ThreadID: "thread-abc",
				},
			},
		}

		err := store.SaveStagedComments(ctx, "feat", comments)
		require.NoError(t, err)

		got, err := store.LoadStagedComments(ctx, "feat")
		require.NoError(t, err)
		require.NotNil(t, got)

		assert.Equal(t, 3, got.NextID)
		assert.Len(t, got.Comments, 2)
		assert.Equal(t, "main.go", got.Comments[0].File)
		assert.Equal(t, 42, got.Comments[0].Line)
		assert.Equal(t, "thread-abc", got.Comments[1].ThreadID)
	})

	t.Run("Overwrite", func(t *testing.T) {
		comments := &state.StagedComments{
			NextID: 2,
			Comments: []state.StagedComment{
				{ID: 1, File: "new.go", Line: 1, Body: "New comment"},
			},
		}

		err := store.SaveStagedComments(ctx, "feat", comments)
		require.NoError(t, err)

		got, err := store.LoadStagedComments(ctx, "feat")
		require.NoError(t, err)
		require.NotNil(t, got)

		assert.Len(t, got.Comments, 1)
		assert.Equal(t, "new.go", got.Comments[0].File)
	})

	t.Run("Clear", func(t *testing.T) {
		err := store.ClearStagedComments(ctx, "feat")
		require.NoError(t, err)

		got, err := store.LoadStagedComments(ctx, "feat")
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("ClearNonExistent", func(t *testing.T) {
		// Clearing a branch with no staged comments
		// should not error.
		err := store.ClearStagedComments(ctx, "nonexistent")
		require.NoError(t, err)
	})

	t.Run("MultipleBranches", func(t *testing.T) {
		commentsA := &state.StagedComments{
			NextID: 2,
			Comments: []state.StagedComment{
				{ID: 1, File: "a.go", Line: 1, Body: "A"},
			},
		}
		commentsB := &state.StagedComments{
			NextID: 2,
			Comments: []state.StagedComment{
				{ID: 1, File: "b.go", Line: 2, Body: "B"},
			},
		}

		require.NoError(t,
			store.SaveStagedComments(ctx, "branch-a", commentsA))
		require.NoError(t,
			store.SaveStagedComments(ctx, "branch-b", commentsB))

		gotA, err := store.LoadStagedComments(ctx, "branch-a")
		require.NoError(t, err)
		assert.Equal(t, "a.go", gotA.Comments[0].File)

		gotB, err := store.LoadStagedComments(ctx, "branch-b")
		require.NoError(t, err)
		assert.Equal(t, "b.go", gotB.Comments[0].File)
	})
}
