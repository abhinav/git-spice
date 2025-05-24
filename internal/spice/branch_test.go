package spice

import (
	"encoding/json"
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
		give string
		want string
	}{
		{"Hello, World!", "hello-world"},
		{"Long message that should be truncated", "long-message-that-should-be"},
		{"1234 5678", "1234-5678"},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			got := GenerateBranchName(tt.give)
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

	shamhubForge := &shamhub.Forge{
		Log: silogtest.New(t),
		Options: shamhub.Options{
			URL:    shamhubServer.URL,
			APIURL: shamhubServer.URL,
		},
	}

	var forgeReg forge.Registry
	forgeReg.Register(shamhubForge)

	// Without a remote set, the Service won't have a Forge connected.
	t.Run("NoRemote", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		mockRepo := NewMockGitRepository(mockCtrl)
		mockStore := NewMockStore(mockCtrl)

		mockStore.EXPECT().
			Remote().
			Return("", git.ErrNotExist).
			AnyTimes()

		mockRepo.EXPECT().
			PeelToCommit(gomock.Any(), "feature").
			Return(git.Hash("def123"), nil).
			AnyTimes()

		svc := NewService(mockRepo, mockStore, &forgeReg, silogtest.New(t))

		// We should still be able to resolve metadata
		// for known forges.
		t.Run("KnownForge", func(t *testing.T) {
			ctx := t.Context()
			mockStore.EXPECT().
				LookupBranch(gomock.Any(), "feature").
				Return(&state.LookupResponse{
					Base:           "main",
					BaseHash:       "abc123",
					ChangeMetadata: json.RawMessage(`{"number": 123}`),
					ChangeForge:    shamhubForge.ID(),
				}, nil)

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
				LookupBranch(gomock.Any(), "feature").
				Return(&state.LookupResponse{
					Base:           "main",
					BaseHash:       "abc123",
					ChangeMetadata: json.RawMessage(`{"number": 123}`),
					ChangeForge:    "unknown",
				}, nil)

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
		mockStore := NewMockStore(mockCtrl)

		mockRepo.EXPECT().
			PeelToCommit(gomock.Any(), "feature").
			Return(git.Hash("def123"), nil).
			AnyTimes()

		mockStore.EXPECT().
			LookupBranch(gomock.Any(), "feature").
			Return(&state.LookupResponse{
				Base:           "main",
				BaseHash:       "abc123",
				ChangeMetadata: json.RawMessage(`{"number": 123`),
				ChangeForge:    shamhubForge.ID(),
			}, nil)

		svc := NewService(mockRepo, mockStore, &forgeReg, silogtest.New(t))
		resp, err := svc.LookupBranch(ctx, "feature")
		require.NoError(t, err)

		assert.Nil(t, resp.Change)
	})
}

func TestService_LookupBranch_upstreamBranch(t *testing.T) {
	ctx := t.Context()

	// Use in-memory storage backend and real store.
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:     storage.NewDB(make(storage.MapBackend)),
		Trunk:  "main",
		Remote: "origin",
		Log:    silogtest.New(t),
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

	// The branch exists in the repo.
	mockRepo.EXPECT().
		PeelToCommit(gomock.Any(), "feature").
		Return(git.Hash("def123"), nil).
		AnyTimes()

	svc := NewService(mockRepo, store, nil /* forges */, silogtest.New(t))

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
}
