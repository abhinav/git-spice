package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/review"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

func TestReviewDrafts(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := storage.NewDB(make(storage.MapBackend))
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    db,
		Trunk: "main",
	})
	require.NoError(t, err)
	tx := store.BeginBranchTx()
	require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
		Name: "feature",
		Base: "main",
	}))
	require.NoError(t, tx.Commit(ctx, "track feature"))

	comment, err := store.AddReviewDraft(
		ctx,
		"feature",
		review.Draft{
			ID:   0,
			Body: "comment body",
			Anchor: review.Anchor{
				Path:      "main.go",
				StartLine: 42,
				EndLine:   42,
			},
		},
	)
	require.NoError(t, err)
	assert.Equal(t, review.DraftID(1), comment.ID)

	reply, err := store.AddReviewDraft(
		ctx,
		"feature",
		review.Draft{ID: 0, Body: "reply body", ReplyTo: "thread-7"},
	)
	require.NoError(t, err)
	assert.Equal(t, review.DraftID(2), reply.ID)

	require.NoError(t, store.UpdateReviewDraftBody(
		ctx,
		"feature",
		comment.ID,
		"updated body",
	))
	drafts, err := store.LoadReviewDrafts(ctx, "feature")
	require.NoError(t, err)
	require.Len(t, drafts, 2)
	assert.Equal(t, "updated body", drafts[0].Body)
	assert.Equal(t, reply, drafts[1])

	require.NoError(t, store.ClearReviewDrafts(ctx, "feature"))
	drafts, err = store.LoadReviewDrafts(ctx, "feature")
	require.NoError(t, err)
	assert.Nil(t, drafts)
}

func TestReviewDraftsFollowBranchLifecycle(t *testing.T) {
	t.Parallel()

	t.Run("Delete", func(t *testing.T) {
		ctx := t.Context()
		db := storage.NewDB(make(storage.MapBackend))
		store, err := state.InitStore(ctx, state.InitStoreRequest{
			DB:    db,
			Trunk: "main",
		})
		require.NoError(t, err)

		tx := store.BeginBranchTx()
		require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
			Name: "feature",
			Base: "main",
		}))
		require.NoError(t, tx.Commit(ctx, "track feature"))

		_, err = store.AddReviewDraft(
			ctx,
			"feature",
			review.Draft{
				ID:   0,
				Body: "comment body",
				Anchor: review.Anchor{
					Path:      "main.go",
					StartLine: 42,
					EndLine:   42,
				},
			},
		)
		require.NoError(t, err)

		tx = store.BeginBranchTx()
		require.NoError(t, tx.Delete(ctx, "feature"))
		require.NoError(t, tx.Commit(ctx, "untrack feature"))

		drafts, err := store.LoadReviewDrafts(ctx, "feature")
		require.NoError(t, err)
		assert.Nil(t, drafts)
	})

	t.Run("Rename", func(t *testing.T) {
		ctx := t.Context()
		db := storage.NewDB(make(storage.MapBackend))
		store, err := state.InitStore(ctx, state.InitStoreRequest{
			DB:    db,
			Trunk: "main",
		})
		require.NoError(t, err)

		tx := store.BeginBranchTx()
		require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
			Name: "feature",
			Base: "main",
		}))
		require.NoError(t, tx.Commit(ctx, "track feature"))

		_, err = store.AddReviewDraft(
			ctx,
			"feature",
			review.Draft{
				ID:   0,
				Body: "comment body",
				Anchor: review.Anchor{
					Path:      "main.go",
					StartLine: 42,
					EndLine:   42,
				},
			},
		)
		require.NoError(t, err)

		tx = store.BeginBranchTx()
		require.NoError(t, tx.Rename(ctx, "feature", "renamed"))
		require.NoError(t, tx.Commit(ctx, "rename feature"))

		drafts, err := store.LoadReviewDrafts(ctx, "feature")
		require.NoError(t, err)
		assert.Nil(t, drafts)

		drafts, err = store.LoadReviewDrafts(ctx, "renamed")
		require.NoError(t, err)
		require.Len(t, drafts, 1)
		assert.Equal(t, "comment body", drafts[0].Body)

		added, err := store.AddReviewDraft(
			ctx,
			"renamed",
			review.Draft{
				ID:   0,
				Body: "second comment",
				Anchor: review.Anchor{
					Path:      "main.go",
					StartLine: 42,
					EndLine:   42,
				},
			},
		)
		require.NoError(t, err)
		assert.Equal(t, review.DraftID(2), added.ID)
	})

	t.Run("Untracked", func(t *testing.T) {
		ctx := t.Context()
		db := storage.NewDB(make(storage.MapBackend))
		store, err := state.InitStore(ctx, state.InitStoreRequest{
			DB:    db,
			Trunk: "main",
		})
		require.NoError(t, err)

		_, err = store.AddReviewDraft(
			ctx,
			"feature",
			review.Draft{
				ID:   0,
				Body: "comment body",
				Anchor: review.Anchor{
					Path:      "main.go",
					StartLine: 42,
					EndLine:   42,
				},
			},
		)
		assert.ErrorIs(t, err, state.ErrNotExist)

		drafts, err := store.LoadReviewDrafts(ctx, "feature")
		require.NoError(t, err)
		assert.Nil(t, drafts)
	})
}
