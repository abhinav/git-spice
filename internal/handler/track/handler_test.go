package track

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/statetest"
)

func TestHandler_AddBranch(t *testing.T) {
	t.Run("CannotTrackTrunk", func(t *testing.T) {
		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, nil)
		store := statetest.NewMemoryStore(t, "main", "", log)

		ctrl := gomock.NewController(t)
		handler := &Handler{
			Log:        log,
			Repository: NewMockGitRepository(ctrl),
			Store:      store,
			Service:    NewMockService(ctrl),
		}

		err := handler.AddBranch(t.Context(), &AddBranchRequest{
			Branch: "main",
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot track trunk branch")
	})

	t.Run("BaseSpecified", func(t *testing.T) {
		log := silog.Nop()
		store := statetest.NewMemoryStore(t, "main", "", log)

		// Track "develop" branch as base.
		require.NoError(t, statetest.UpdateBranch(t.Context(), store, &statetest.UpdateRequest{
			Upserts: []state.UpsertRequest{
				{
					Name:     "develop",
					Base:     "main",
					BaseHash: git.Hash("abc123"),
				},
			},
			Message: "add feature branch for test",
		}))

		ctrl := gomock.NewController(t)

		mockRepo := NewMockGitRepository(ctrl)
		mockRepo.EXPECT().
			PeelToCommit(t.Context(), "develop").
			Return(git.Hash("def456"), nil)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			VerifyRestacked(t.Context(), "feature").
			Return(nil)

		handler := &Handler{
			Log:        log,
			Repository: mockRepo,
			Store:      store,
			Service:    mockService,
		}

		err := handler.AddBranch(t.Context(), &AddBranchRequest{
			Branch: "feature",
			Base:   "develop",
		})
		require.NoError(t, err)
	})

	t.Run("NoTrackedBranches", func(t *testing.T) {
		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, nil)
		store := statetest.NewMemoryStore(t, "main", "", log)

		ctrl := gomock.NewController(t)

		mockRepo := NewMockGitRepository(ctrl)
		mockRepo.EXPECT().
			PeelToCommit(t.Context(), "main").
			Return(git.Hash("def456"), nil)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			VerifyRestacked(t.Context(), "feature").Return(nil)

		handler := &Handler{
			Log:        log,
			Repository: mockRepo,
			Store:      store,
			Service:    mockService,
		}

		err := handler.AddBranch(t.Context(), &AddBranchRequest{
			Branch: "feature",
		})
		require.NoError(t, err)
		assert.Contains(t, logBuffer.String(), "feature: using base branch: main")
	})

	t.Run("GuessFailureDoesNotBreak", func(t *testing.T) {
		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, nil)
		store := statetest.NewMemoryStore(t, "main", "", log)

		ctx := t.Context()
		err := statetest.UpdateBranch(ctx, store, &statetest.UpdateRequest{
			Upserts: []state.UpsertRequest{
				{
					Name:     "existing-branch",
					Base:     "main",
					BaseHash: git.Hash("existing123"),
				},
			},
			Message: "add existing branch for test",
		})
		require.NoError(t, err)

		ctrl := gomock.NewController(t)

		mockRepo := NewMockGitRepository(ctrl)
		mockRepo.EXPECT().
			PeelToCommit(ctx, "new-feature").
			Return("", assert.AnError)
		mockRepo.EXPECT().
			PeelToCommit(ctx, "main").
			Return(git.Hash("main456"), nil)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			VerifyRestacked(ctx, "new-feature").
			Return(nil)

		handler := &Handler{
			Log:        log,
			Repository: mockRepo,
			Store:      store,
			Service:    mockService,
		}

		err = handler.AddBranch(ctx, &AddBranchRequest{
			Branch: "new-feature",
		})
		require.NoError(t, err)
		assert.Contains(t, logBuffer.String(), "could not guess base branch, using trunk")
		assert.Contains(t, logBuffer.String(), "new-feature: using base branch: main")
	})

	t.Run("NeedsRestack", func(t *testing.T) {
		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, nil)
		store := statetest.NewMemoryStore(t, "main", "", log)

		ctrl := gomock.NewController(t)

		mockRepo := NewMockGitRepository(ctrl)
		mockRepo.EXPECT().
			PeelToCommit(t.Context(), "main").
			Return(git.Hash("main123"), nil)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			VerifyRestacked(t.Context(), "feature").
			Return(&spice.BranchNeedsRestackError{
				Base:     "main",
				BaseHash: git.Hash("main123"),
			})

		handler := &Handler{
			Log:        log,
			Repository: mockRepo,
			Store:      store,
			Service:    mockService,
		}

		err := handler.AddBranch(t.Context(), &AddBranchRequest{
			Branch: "feature",
			Base:   "main",
		})
		require.NoError(t, err)
		assert.Contains(t, logBuffer.String(), "branch is behind its base and needs to be restacked")
	})

	t.Run("RestackVerificationFailure", func(t *testing.T) {
		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, nil)
		store := statetest.NewMemoryStore(t, "main", "", log)

		ctrl := gomock.NewController(t)

		mockRepo := NewMockGitRepository(ctrl)
		mockRepo.EXPECT().
			PeelToCommit(t.Context(), "main").
			Return(git.Hash("main123"), nil)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			VerifyRestacked(t.Context(), "feature").
			Return(assert.AnError)

		handler := &Handler{
			Log:        log,
			Repository: mockRepo,
			Store:      store,
			Service:    mockService,
		}

		err := handler.AddBranch(t.Context(), &AddBranchRequest{
			Branch: "feature",
			Base:   "main",
		})
		require.NoError(t, err)
		assert.Contains(t, logBuffer.String(), "stack state verification failed")
	})
}
