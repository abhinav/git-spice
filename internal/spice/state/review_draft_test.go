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

	anchor, err := review.NewLineAnchor("main.go", 42)
	require.NoError(t, err)
	comment, err := store.AddReviewDraft(
		ctx,
		"feature",
		review.NewCommentDraft(0, anchor, "comment body"),
	)
	require.NoError(t, err)
	assert.Equal(t, review.DraftID(1), comment.ID())

	reply, err := store.AddReviewDraft(
		ctx,
		"feature",
		review.NewReplyDraft(0, "thread-7", "reply body"),
	)
	require.NoError(t, err)
	assert.Equal(t, review.DraftID(2), reply.ID())

	require.NoError(t, store.UpdateReviewDraftBody(
		ctx,
		"feature",
		comment.ID(),
		"updated body",
	))
	drafts, err := store.LoadReviewDrafts(ctx, "feature")
	require.NoError(t, err)
	require.Len(t, drafts, 2)
	assert.Equal(t, "updated body", drafts[0].Body())
	assert.Equal(t, reply, drafts[1])

	require.NoError(t, store.ClearReviewDrafts(ctx, "feature"))
	drafts, err = store.LoadReviewDrafts(ctx, "feature")
	require.NoError(t, err)
	assert.Nil(t, drafts)
}
