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
	"go.abhg.dev/gs/internal/logutil"
	"go.abhg.dev/gs/internal/spice/state"
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
		Log: logutil.TestLogger(t),
		Options: shamhub.Options{
			URL:    shamhubServer.URL,
			APIURL: shamhubServer.URL,
		},
	}
	t.Cleanup(forge.Register(shamhubForge))

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

		svc := NewService(mockRepo, mockStore, logutil.TestLogger(t))

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

		svc := NewService(mockRepo, mockStore, logutil.TestLogger(t))
		resp, err := svc.LookupBranch(ctx, "feature")
		require.NoError(t, err)

		assert.Nil(t, resp.Change)
	})
}
