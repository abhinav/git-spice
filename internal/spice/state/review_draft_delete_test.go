package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/review"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

func TestReviewDraftsDelete(t *testing.T) {
	ctx := t.Context()
	db := storage.NewDB(make(storage.MapBackend))
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    db,
		Trunk: "main",
	})
	require.NoError(t, err)
	tx := store.BeginBranchTx()
	require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
		Name: "feat",
		Base: "main",
	}))
	require.NoError(t, tx.Commit(ctx, "track feat"))

	var added []review.Draft
	for _, body := range []string{"First", "Second", "Third"} {
		draft, err := store.AddReviewDraft(
			ctx,
			"feat",
			review.Draft{
				ID:   0,
				Body: body,
				Anchor: review.Anchor{
					Path:      "main.go",
					StartLine: 1,
					EndLine:   1,
				},
			},
		)
		require.NoError(t, err)
		added = append(added, draft)
	}

	deleted, err := store.DeleteReviewDrafts(
		ctx,
		"feat",
		[]review.DraftID{1, 3, 3},
	)
	require.NoError(t, err)
	assert.Equal(t, 2, deleted)
	drafts, err := store.LoadReviewDrafts(ctx, "feat")
	require.NoError(t, err)
	require.NotNil(t, drafts)
	assert.Equal(t, []review.Draft{added[1]}, drafts)

	_, err = store.DeleteReviewDrafts(
		ctx,
		"feat",
		[]review.DraftID{2, 99},
	)
	assert.EqualError(t, err, "draft comment 99 not found")
	drafts, err = store.LoadReviewDrafts(ctx, "feat")
	require.NoError(t, err)
	assert.Equal(t, []review.Draft{added[1]}, drafts)
}

func TestReviewDraftsDeleteFromUntrackedBranch(t *testing.T) {
	ctx := t.Context()
	db := storage.NewDB(make(storage.MapBackend))
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    db,
		Trunk: "main",
	})
	require.NoError(t, err)

	_, err = store.DeleteReviewDrafts(
		ctx,
		"feat",
		[]review.DraftID{1},
	)
	assert.ErrorIs(t, err, state.ErrNotExist)
}
