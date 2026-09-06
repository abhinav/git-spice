package state_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/review"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

func TestReviewDraftsConcurrentAdd(t *testing.T) {
	ctx := t.Context()
	stores, repo := newConcurrentReviewDraftStores(t)

	added := make([]review.Draft, len(stores))
	// Hold both initial compare-and-swap attempts until they have derived
	// mutations from the same storage commit.
	repo.pauseNextRefUpdates(2)
	errs := runConcurrently(
		func() (err error) {
			added[0], err = stores[0].AddReviewDraft(
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
			return err
		},
		func() (err error) {
			added[1], err = stores[1].AddReviewDraft(
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
			return err
		},
	)
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])

	drafts, err := stores[0].LoadReviewDrafts(ctx, "feat")
	require.NoError(t, err)
	require.NotNil(t, drafts)
	assert.ElementsMatch(
		t,
		[]review.DraftID{1, 2},
		[]review.DraftID{added[0].ID, added[1].ID},
	)
	assert.ElementsMatch(t, added, drafts)
}

func TestReviewDraftsConcurrentEdit(t *testing.T) {
	ctx := t.Context()
	stores, repo := newConcurrentReviewDraftStores(t)
	_, err := stores[0].AddReviewDraft(
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
	_, err = stores[0].AddReviewDraft(
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

	// Hold both initial compare-and-swap attempts until they have derived
	// mutations from the same storage commit.
	repo.pauseNextRefUpdates(2)
	errs := runConcurrently(
		func() error {
			return stores[0].UpdateReviewDraftBody(
				ctx, "feat", 1, "First edited",
			)
		},
		func() error {
			return stores[1].UpdateReviewDraftBody(
				ctx, "feat", 2, "Second edited",
			)
		},
	)
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])

	drafts, err := stores[0].LoadReviewDrafts(ctx, "feat")
	require.NoError(t, err)
	require.NotNil(t, drafts)
	assert.Equal(t, "First edited", drafts[0].Body)
	assert.Equal(t, "Second edited", drafts[1].Body)
}

func newConcurrentReviewDraftStores(
	t *testing.T,
) ([2]*state.Store, *pausingGitRepository) {
	t.Helper()
	ctx := t.Context()
	repo, _, err := git.Init(ctx, t.TempDir(), git.InitOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	pausingRepo := &pausingGitRepository{GitRepository: repo}
	newDB := func() *storage.DB {
		return storage.NewDB(storage.NewGitBackend(storage.GitConfig{
			Repo:        pausingRepo,
			Ref:         "refs/data",
			AuthorName:  "Test Author",
			AuthorEmail: "test@example.com",
			Log:         silogtest.New(t),
		}))
	}
	dbs := [2]*storage.DB{newDB(), newDB()}
	_, err = state.InitStore(ctx, state.InitStoreRequest{
		DB:    dbs[0],
		Trunk: "main",
	})
	require.NoError(t, err)

	var stores [2]*state.Store
	for i, db := range dbs {
		stores[i], err = state.OpenStore(ctx, db, silogtest.New(t))
		require.NoError(t, err)
	}
	return stores, pausingRepo
}

func runConcurrently(operations ...func() error) []error {
	var wg sync.WaitGroup
	errs := make([]error, len(operations))
	for i, operation := range operations {
		wg.Go(func() {
			errs[i] = operation()
		})
	}
	wg.Wait()
	return errs
}

type pausingGitRepository struct {
	storage.GitRepository

	mu      sync.Mutex
	waitFor int
	ready   int
	release chan struct{}
}

func (r *pausingGitRepository) pauseNextRefUpdates(count int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.waitFor = count
	r.ready = 0
	r.release = make(chan struct{})
}

func (r *pausingGitRepository) SetRef(
	ctx context.Context,
	req git.SetRefRequest,
) error {
	r.mu.Lock()
	wait := r.ready < r.waitFor
	if wait {
		r.ready++
		if r.ready == r.waitFor {
			close(r.release)
		}
	}
	release := r.release
	r.mu.Unlock()

	if wait {
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return r.GitRepository.SetRef(ctx, req)
}
