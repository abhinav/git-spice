package spice

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/review"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/statetest"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

func TestService_ForgetBranch_reparentsAndPreservesState(t *testing.T) {
	ctx := t.Context()
	log := silogtest.New(t)
	repo, wt, err := git.Init(ctx, t.TempDir(), git.InitOptions{Log: log})
	require.NoError(t, err)
	mem := make(storage.MapBackend)
	store, err := state.InitStore(ctx, state.InitStoreRequest{DB: storage.NewDB(mem), Trunk: "main", Log: log})
	require.NoError(t, err)
	require.NoError(t, statetest.UpdateBranch(ctx, store, &statetest.UpdateRequest{
		Upserts: []state.UpsertRequest{
			{Name: "root", Base: "main", BaseHash: "recorded"},
			{Name: "child", Base: "root", ChangeForge: "shamhub", ChangeMetadata: jsontext.Value(`{"number":42}`)},
		}, Message: "seed stack",
	}))
	_, err = store.AddReviewDraft(ctx, "root", review.Draft{ID: 0, Body: "root draft"})
	require.NoError(t, err)
	_, err = store.AddReviewDraft(ctx, "child", review.Draft{ID: 0, Body: "child draft"})
	require.NoError(t, err)
	service := NewService(repo, wt, store, nil, log)
	require.NoError(t, service.ForgetBranch(ctx, "root"))
	_, err = store.LookupBranch(ctx, "root")
	assert.ErrorIs(t, err, state.ErrNotExist)
	child, err := store.LookupBranch(ctx, "child")
	require.NoError(t, err)
	assert.Equal(t, "main", child.Base)
	assert.Equal(t, "recorded", child.BaseHash.String())
	assert.JSONEq(t, `{"number":42}`, child.ChangeMetadata.String())
	drafts, err := store.LoadReviewDrafts(ctx, "root")
	require.NoError(t, err)
	assert.Empty(t, drafts)
	drafts, err = store.LoadReviewDrafts(ctx, "child")
	require.NoError(t, err)
	require.Len(t, drafts, 1)
	assert.Equal(t, "child draft", drafts[0].Body)

	mem["branches/corrupt"] = []byte(`{`)
	require.NoError(t, service.ForgetBranch(ctx, "root"), "untracking an unknown branch remains a no-op")
}
