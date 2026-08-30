package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/review"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

func TestReviewDraftsPublishPreservesAddedDraft(t *testing.T) {
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

	_, err = store.AddReviewDraft(
		ctx,
		"feat",
		review.Draft{
			ID:   0,
			Body: "First",
			Anchor: review.Anchor{
				Path:      "first.go",
				StartLine: 1,
				EndLine:   1,
			},
		},
	)
	require.NoError(t, err)
	published, err := store.LoadReviewDrafts(ctx, "feat")
	require.NoError(t, err)

	added, err := store.AddReviewDraft(
		ctx,
		"feat",
		review.Draft{
			ID:   0,
			Body: "Second",
			Anchor: review.Anchor{
				Path:      "second.go",
				StartLine: 2,
				EndLine:   2,
			},
		},
	)
	require.NoError(t, err)
	require.NoError(t, store.RemovePublishedReviewDrafts(
		ctx, "feat", published,
	))

	drafts, err := store.LoadReviewDrafts(ctx, "feat")
	require.NoError(t, err)
	require.NotNil(t, drafts)
	assert.Equal(t, []review.Draft{added}, drafts)
}

func TestReviewDraftsPublishPreservesEditedDraft(t *testing.T) {
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

	added, err := store.AddReviewDraft(
		ctx,
		"feat",
		review.Draft{
			ID:   0,
			Body: "First",
			Anchor: review.Anchor{
				Path:      "first.go",
				StartLine: 1,
				EndLine:   1,
			},
		},
	)
	require.NoError(t, err)
	published, err := store.LoadReviewDrafts(ctx, "feat")
	require.NoError(t, err)

	require.NoError(t, store.UpdateReviewDraftBody(
		ctx, "feat", added.ID, "First edited",
	))
	require.NoError(t, store.RemovePublishedReviewDrafts(
		ctx, "feat", published,
	))

	drafts, err := store.LoadReviewDrafts(ctx, "feat")
	require.NoError(t, err)
	require.NotNil(t, drafts)
	edited := added
	edited.Body = "First edited"
	assert.Equal(t, []review.Draft{edited}, drafts)
}

func TestReviewDraftsPublishAfterBranchDeletion(t *testing.T) {
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

	_, err = store.AddReviewDraft(
		ctx,
		"feat",
		review.Draft{
			ID:   0,
			Body: "First",
			Anchor: review.Anchor{
				Path:      "first.go",
				StartLine: 1,
				EndLine:   1,
			},
		},
	)
	require.NoError(t, err)
	published, err := store.LoadReviewDrafts(ctx, "feat")
	require.NoError(t, err)

	tx = store.BeginBranchTx()
	require.NoError(t, tx.Delete(ctx, "feat"))
	require.NoError(t, tx.Commit(ctx, "untrack feat"))
	require.NoError(t, store.RemovePublishedReviewDrafts(
		ctx,
		"feat",
		published,
	))

	drafts, err := store.LoadReviewDrafts(ctx, "feat")
	require.NoError(t, err)
	assert.Nil(t, drafts)
}

func TestReviewDraftsPublishPreservesNextID(t *testing.T) {
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

	_, err = store.AddReviewDraft(
		ctx,
		"feat",
		review.Draft{
			ID:   0,
			Body: "First",
			Anchor: review.Anchor{
				Path:      "first.go",
				StartLine: 1,
				EndLine:   1,
			},
		},
	)
	require.NoError(t, err)
	_, err = store.AddReviewDraft(
		ctx,
		"feat",
		review.Draft{
			ID:   0,
			Body: "Second",
			Anchor: review.Anchor{
				Path:      "second.go",
				StartLine: 2,
				EndLine:   2,
			},
		},
	)
	require.NoError(t, err)
	published, err := store.LoadReviewDrafts(ctx, "feat")
	require.NoError(t, err)

	require.NoError(t, store.RemovePublishedReviewDrafts(
		ctx, "feat", published,
	))

	drafts, err := store.LoadReviewDrafts(ctx, "feat")
	require.NoError(t, err)
	require.NotNil(t, drafts)
	assert.Empty(t, drafts)

	next, err := store.AddReviewDraft(
		ctx,
		"feat",
		review.Draft{
			ID:   0,
			Body: "Third",
			Anchor: review.Anchor{
				Path:      "first.go",
				StartLine: 1,
				EndLine:   1,
			},
		},
	)
	require.NoError(t, err)
	assert.Equal(t, review.DraftID(3), next.ID)
}
