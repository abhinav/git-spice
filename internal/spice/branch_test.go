package spice

import (
	"encoding/json/jsontext"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/shamhub"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/statetest"
	"go.abhg.dev/gs/internal/spice/state/storage"
	gomock "go.uber.org/mock/gomock"
)

func TestGenerateBranchName(t *testing.T) {
	tests := []struct {
		give        string
		lengthLimit int
		want        string
	}{
		{"Hello, World!", 32, "hello-world"},
		{"Long message that should be truncated", 32, "long-message-that-should-be"},
		{"Long message that should not be truncated", 64, "long-message-that-should-not-be-truncated"},
		{"1234 5678", 32, "1234-5678"},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			got := GenerateBranchName(tt.give, tt.lengthLimit)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestService_LookupBranch_changeAssociation(t *testing.T) {
	// This test should not make real requests to the server,
	// but we need a real URL to work with for matching.
	shamhubServer := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("Unexpected request: %s %s", r.Method, r.URL)
	}))
	t.Cleanup(shamhubServer.Close)

	shamhubForge := &shamhub.Definition{
		Log:    silogtest.New(t),
		URL:    shamhubServer.URL,
		APIURL: shamhubServer.URL,
	}

	var forgeReg forge.Registry
	forgeReg.Register(shamhubForge)

	// Without a remote set, the Service won't have a Forge connected.
	t.Run("NoRemote", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		mockRepo := NewMockGitRepository(mockCtrl)
		mockWT := NewMockGitWorktree(mockCtrl)
		mockStore := NewMockStore(mockCtrl)

		mockStore.EXPECT().
			Remote().
			Return(state.Remote{}, git.ErrNotExist).
			AnyTimes()

		mockRepo.EXPECT().
			PeelToCommit(gomock.Any(), "feature").
			Return(git.Hash("def123"), nil).
			AnyTimes()

		svc := NewService(mockRepo, mockWT, mockStore, &forgeReg, silogtest.New(t))

		// We should still be able to resolve metadata
		// for known forges.
		t.Run("KnownForge", func(t *testing.T) {
			ctx := t.Context()
			mockStore.EXPECT().
				LookupBranches(gomock.Any(), []string{"feature"}).
				Return([]*state.LookupResponse{{
					Base:           "main",
					BaseHash:       "abc123",
					ChangeMetadata: jsontext.Value(`{"number": 123}`),
					ChangeForge:    shamhubForge.ID(),
				}}, nil)

			resp, err := svc.LookupBranch(ctx, "feature")
			require.NoError(t, err)

			assert.Equal(t, &shamhub.ChangeMetadata{
				Number: 123,
			}, resp.Change)
		})

		// And should not fail for unknown forges.
		t.Run("UnknownForge", func(t *testing.T) {
			ctx := t.Context()
			mockStore.EXPECT().
				LookupBranches(gomock.Any(), []string{"feature"}).
				Return([]*state.LookupResponse{{
					Base:           "main",
					BaseHash:       "abc123",
					ChangeMetadata: jsontext.Value(`{"number": 123}`),
					ChangeForge:    "unknown",
				}}, nil)

			resp, err := svc.LookupBranch(ctx, "feature")
			require.NoError(t, err)

			assert.Equal(t, "main", resp.Base)
			assert.Equal(t, git.Hash("abc123"), resp.BaseHash)
			assert.Nil(t, resp.Change)
		})
	})

	t.Run("CorruptedMetadata", func(t *testing.T) {
		ctx := t.Context()
		mockCtrl := gomock.NewController(t)
		mockRepo := NewMockGitRepository(mockCtrl)
		mockWT := NewMockGitWorktree(mockCtrl)
		mockStore := NewMockStore(mockCtrl)

		mockRepo.EXPECT().
			PeelToCommit(gomock.Any(), "feature").
			Return(git.Hash("def123"), nil).
			AnyTimes()

		mockStore.EXPECT().
			LookupBranches(gomock.Any(), []string{"feature"}).
			Return([]*state.LookupResponse{{
				Base:           "main",
				BaseHash:       "abc123",
				ChangeMetadata: jsontext.Value(`{"number": 123`),
				ChangeForge:    shamhubForge.ID(),
			}}, nil)

		svc := NewService(mockRepo, mockWT, mockStore, &forgeReg, silogtest.New(t))
		resp, err := svc.LookupBranch(ctx, "feature")
		require.NoError(t, err)

		assert.Nil(t, resp.Change)
	})
}

func TestService_LookupBranch_upstreamBranch(t *testing.T) {
	ctx := t.Context()

	// Use in-memory storage backend and real store.
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    storage.NewDB(make(storage.MapBackend)),
		Trunk: "main",
		Remote: state.Remote{
			Upstream: "origin",
			Push:     "origin",
		},
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	// Branch will always exist in the store for this test.
	require.NoError(t, statetest.UpdateBranch(ctx, store, &statetest.UpdateRequest{
		Upserts: []state.UpsertRequest{{
			Name:     "feature",
			Base:     "main",
			BaseHash: "abc123",
		}},
	}), "failed to add test branch to the store")

	// setUpstreamBranch may be called from a function below
	// to set or clear the upstream branch.
	setUpstreamBranch := func(upstream string) {
		require.NoError(t, statetest.UpdateBranch(ctx, store, &statetest.UpdateRequest{
			Upserts: []state.UpsertRequest{{
				Name:           "feature",
				UpstreamBranch: &upstream,
			}},
		}))
	}

	mockCtrl := gomock.NewController(t)
	mockRepo := NewMockGitRepository(mockCtrl)
	mockWT := NewMockGitWorktree(mockCtrl)

	// The branch exists in the repo.
	mockRepo.EXPECT().
		PeelToCommit(gomock.Any(), "feature").
		Return(git.Hash("def123"), nil).
		AnyTimes()

	svc := NewService(mockRepo, mockWT, store, nil /* forges */, silogtest.New(t))

	t.Run("NoUpstream", func(t *testing.T) {
		setUpstreamBranch("")

		// The branch exists, but has no upstream.
		resp, err := svc.LookupBranch(ctx, "feature")
		require.NoError(t, err)
		assert.Equal(t, "main", resp.Base)
		assert.Equal(t, git.Hash("abc123"), resp.BaseHash)
		assert.Empty(t, resp.UpstreamBranch)
	})

	t.Run("UpstreamExists", func(t *testing.T) {
		setUpstreamBranch("feature")

		// Upstream branch ref exists.
		mockRepo.EXPECT().
			PeelToCommit(gomock.Any(), "origin/feature").
			Return(git.Hash("def123"), nil)

		resp, err := svc.LookupBranch(ctx, "feature")
		require.NoError(t, err)
		assert.Equal(t, "main", resp.Base)
		assert.Equal(t, git.Hash("abc123"), resp.BaseHash)
		assert.Equal(t, "feature", resp.UpstreamBranch)
	})

	t.Run("UpstreamRefDeleted", func(t *testing.T) {
		setUpstreamBranch("feature")

		// Upstream branch ref was deleted out of band.
		mockRepo.EXPECT().
			PeelToCommit(gomock.Any(), "origin/feature").
			Return(git.Hash(""), git.ErrNotExist)

		resp, err := svc.LookupBranch(ctx, "feature")
		require.NoError(t, err)
		assert.Equal(t, "main", resp.Base)
		assert.Equal(t, git.Hash("abc123"), resp.BaseHash)
		assert.Empty(t, resp.UpstreamBranch,
			"UpstreamBranch should be cleared if deleted out of band")

		// Also verify that the store was updated.
		lookup, err := store.LookupBranch(ctx, "feature")
		require.NoError(t, err)
		assert.Empty(t, lookup.UpstreamBranch)
	})

	t.Run("UpstreamRefWithDifferentName", func(t *testing.T) {
		setUpstreamBranch("user/feature")

		// Upstream branch still exists.
		mockRepo.EXPECT().
			PeelToCommit(gomock.Any(), "origin/user/feature").
			Return(git.Hash("def123"), nil)

		resp, err := svc.LookupBranch(ctx, "feature")
		require.NoError(t, err)
		assert.Equal(t, "main", resp.Base)
		assert.Equal(t, git.Hash("abc123"), resp.BaseHash)
		assert.Equal(t, "user/feature", resp.UpstreamBranch)
	})

	t.Run("UpstreamRefDeletedWithDifferentName", func(t *testing.T) {
		setUpstreamBranch("user/feature")

		// Upstream branch ref was deleted out of band.
		mockRepo.EXPECT().
			PeelToCommit(gomock.Any(), "origin/user/feature").
			Return(git.Hash(""), git.ErrNotExist)

		resp, err := svc.LookupBranch(ctx, "feature")
		require.NoError(t, err)
		assert.Equal(t, "main", resp.Base)
		assert.Equal(t, git.Hash("abc123"), resp.BaseHash)
		assert.Empty(t, resp.UpstreamBranch,
			"UpstreamBranch should be cleared if deleted out of band")

		// Also verify that the store was updated.
		lookup, err := store.LookupBranch(ctx, "feature")
		require.NoError(t, err)
		assert.Empty(t, lookup.UpstreamBranch)
	})

	t.Run("ForkModeUsesPushRemote", func(t *testing.T) {
		require.NoError(t, store.SetRemote(ctx, state.Remote{
			Upstream: "upstream",
			Push:     "origin",
		}))
		setUpstreamBranch("feature")

		// Submitted branches are pushed to the push remote in fork mode,
		// so the upstream branch name must be validated there.
		mockRepo.EXPECT().
			PeelToCommit(gomock.Any(), "origin/feature").
			Return(git.Hash("def123"), nil)

		resp, err := svc.LookupBranch(ctx, "feature")
		require.NoError(t, err)
		assert.Equal(t, "feature", resp.UpstreamBranch)
	})
}

// Regression test for https://github.com/abhinav/git-spice/issues/926
func TestService_LoadBranches_lookupError_issue926(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockStore := NewMockStore(mockCtrl)

	mockStore.EXPECT().
		ListBranches(gomock.Any()).
		Return(func(yield func(string, error) bool) {
			yield("feature", nil)
		})

	mockStore.EXPECT().
		LookupBranches(gomock.Any(), []string{"feature"}).
		Return(nil, assert.AnError)

	mockRepo := NewMockGitRepository(mockCtrl)

	svc := NewService(
		mockRepo,
		NewMockGitWorktree(mockCtrl),
		mockStore,
		nil,
		silogtest.New(t),
	)
	ctx := t.Context()
	_, err := svc.LoadBranches(ctx)
	assert.ErrorIs(t, err, assert.AnError)
}

func TestService_LookupBranches_mixedOutcomes(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	mockStore := NewMockStore(mockCtrl)
	names := []string{"feature", "deleted", "untracked", "unknown", "feature"}
	mockStore.EXPECT().
		LookupBranches(gomock.Any(), names).
		Return([]*state.LookupResponse{
			{Base: "main", BaseHash: "abc123"},
			{Base: "feature", BaseHash: "def456"},
			nil,
			nil,
			{Base: "main", BaseHash: "abc123"},
		}, &storage.MissingKeysError{
			Indexes: []int{2, 3},
			Keys:    []string{"branches/untracked", "branches/unknown"},
		})
	mockRepo := NewMockGitRepository(mockCtrl)
	mockRepo.EXPECT().
		LocalBranches(gomock.Any(), &git.LocalBranchesOptions{Patterns: names}).
		Return(func(yield func(git.LocalBranch, error) bool) {
			if !yield(git.LocalBranch{Name: "feature", Hash: "fff111"}, nil) {
				return
			}
			yield(git.LocalBranch{Name: "untracked", Hash: "fff222"}, nil)
		})
	svc := NewService(mockRepo, NewMockGitWorktree(mockCtrl), mockStore, nil, silogtest.New(t))

	results, err := svc.LookupBranches(t.Context(), names)
	require.NoError(t, err)
	require.Len(t, results, len(names))
	require.NoError(t, results[0].Err)
	assert.Equal(t, git.Hash("fff111"), results[0].Response.Head)
	var deleted *DeletedBranchError
	require.ErrorAs(t, results[1].Err, &deleted)
	assert.Equal(t, "deleted", deleted.Name)
	assert.Equal(t, "feature", deleted.Base)
	assert.ErrorIs(t, results[2].Err, state.ErrNotExist)
	assert.ErrorIs(t, results[3].Err, git.ErrNotExist)
	require.NoError(t, results[4].Err)
	assert.Equal(t, results[0].Response, results[4].Response)
}

func TestService_LookupBranch_missing(t *testing.T) {
	tests := []struct {
		name    string
		gitErr  error
		wantErr error
	}{
		{name: "Untracked", wantErr: state.ErrNotExist},
		{name: "Unknown", gitErr: git.ErrNotExist, wantErr: git.ErrNotExist},
		{name: "ResolutionFailure", gitErr: assert.AnError, wantErr: assert.AnError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			mockStore := NewMockStore(mockCtrl)
			mockStore.EXPECT().
				LookupBranches(gomock.Any(), []string{"feature"}).
				Return([]*state.LookupResponse{nil}, &storage.MissingKeysError{
					Indexes: []int{0},
					Keys:    []string{"branches/feature"},
				})
			mockRepo := NewMockGitRepository(mockCtrl)
			mockRepo.EXPECT().
				PeelToCommit(gomock.Any(), "feature").
				Return(git.Hash("abc123"), tt.gitErr)
			svc := NewService(mockRepo, NewMockGitWorktree(mockCtrl), mockStore, nil, silogtest.New(t))
			resp, err := svc.LookupBranch(t.Context(), "feature")
			assert.Nil(t, resp)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestService_LookupBranch_resolutionError(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	mockStore := NewMockStore(mockCtrl)
	mockStore.EXPECT().
		LookupBranches(gomock.Any(), []string{"feature"}).
		Return([]*state.LookupResponse{{Base: "main", BaseHash: "abc123"}}, nil)
	mockRepo := NewMockGitRepository(mockCtrl)
	mockRepo.EXPECT().
		PeelToCommit(gomock.Any(), "feature").
		Return(git.Hash(""), assert.AnError)
	svc := NewService(mockRepo, NewMockGitWorktree(mockCtrl), mockStore, nil, silogtest.New(t))

	resp, err := svc.LookupBranch(t.Context(), "feature")
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, assert.AnError)
}

func TestService_LookupBranches_listError(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	mockStore := NewMockStore(mockCtrl)
	mockStore.EXPECT().
		LookupBranches(gomock.Any(), []string{"a", "b"}).
		Return([]*state.LookupResponse{{Base: "main"}, {Base: "main"}}, nil)
	mockRepo := NewMockGitRepository(mockCtrl)
	mockRepo.EXPECT().
		LocalBranches(gomock.Any(), gomock.Any()).
		Return(func(yield func(git.LocalBranch, error) bool) {
			yield(git.LocalBranch{}, assert.AnError)
		})
	svc := NewService(mockRepo, NewMockGitWorktree(mockCtrl), mockStore, nil, silogtest.New(t))
	results, err := svc.LookupBranches(t.Context(), []string{"a", "b"})
	assert.Nil(t, results)
	assert.ErrorIs(t, err, assert.AnError)
}

func TestService_LoadBranches_usesLookupBranches(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockStore := NewMockStore(mockCtrl)
	mockStore.EXPECT().
		ListBranches(gomock.Any()).
		Return(func(yield func(string, error) bool) {
			yield("feature-b", nil)
			yield("feature-a", nil)
		})

	mockStore.EXPECT().
		LookupBranches(gomock.Any(), []string{"feature-b", "feature-a"}).
		Return([]*state.LookupResponse{
			{
				Base:     "feature-a",
				BaseHash: git.Hash("aaa111"),
			},
			{
				Base:     "main",
				BaseHash: git.Hash("bbb222"),
			},
		}, nil)

	mockRepo := NewMockGitRepository(mockCtrl)
	mockRepo.EXPECT().
		LocalBranches(gomock.Any(), gomock.Any()).
		Return(func(yield func(git.LocalBranch, error) bool) {
			if !yield(git.LocalBranch{Name: "feature-a", Hash: "abc123"}, nil) {
				return
			}
			if !yield(git.LocalBranch{Name: "untracked", Hash: "fff999"}, nil) {
				return
			}
			yield(git.LocalBranch{Name: "feature-b", Hash: "def456"}, nil)
		})

	svc := NewService(
		mockRepo,
		NewMockGitWorktree(mockCtrl),
		mockStore,
		nil,
		silogtest.New(t),
	)

	got, err := svc.LoadBranches(t.Context())
	require.NoError(t, err)

	assert.Equal(t, []LoadBranchItem{
		{
			Name:     "feature-a",
			Head:     git.Hash("abc123"),
			Base:     "main",
			BaseHash: git.Hash("bbb222"),
		},
		{
			Name:     "feature-b",
			Head:     git.Hash("def456"),
			Base:     "feature-a",
			BaseHash: git.Hash("aaa111"),
		},
	}, got)
}
