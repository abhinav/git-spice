package checkout

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/track"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.uber.org/mock/gomock"
)

func TestHandler_CheckoutBranch_Trunk(t *testing.T) {
	mockStore := NewMockStore(gomock.NewController(t))
	mockStore.
		EXPECT().
		Trunk().
		Return("main").
		AnyTimes()

	t.Run("Normal", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockWorktree := NewMockGitWorktree(ctrl)

		handler := &Handler{
			Stdout:     io.Discard,
			Log:        silog.Nop(),
			Store:      mockStore,
			Repository: NewMockGitRepository(ctrl),
			Worktree:   mockWorktree,
			Track:      NewMockTrackHandler(ctrl),
			Service:    NewMockService(ctrl),
		}

		mockWorktree.
			EXPECT().
			CheckoutBranch(gomock.Any(), "main").
			Return(nil)

		err := handler.CheckoutBranch(t.Context(), &Request{
			Branch: "main",
		})
		assert.NoError(t, err)
	})

	t.Run("DryRun", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		var stdout bytes.Buffer
		handler := &Handler{
			Stdout:     &stdout,
			Log:        silog.Nop(),
			Store:      mockStore,
			Repository: NewMockGitRepository(ctrl),
			Worktree:   NewMockGitWorktree(ctrl),
			Track:      NewMockTrackHandler(ctrl),
			Service:    NewMockService(ctrl),
		}

		err := handler.CheckoutBranch(t.Context(), &Request{
			Branch:  "main",
			Options: &Options{DryRun: true},
		})
		assert.NoError(t, err)
		assert.Equal(t, "main\n", stdout.String())
	})

	t.Run("Detach", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockWorktree := NewMockGitWorktree(ctrl)

		handler := &Handler{
			Stdout:     io.Discard,
			Log:        silog.Nop(),
			Store:      mockStore,
			Repository: NewMockGitRepository(ctrl),
			Worktree:   mockWorktree,
			Track:      NewMockTrackHandler(ctrl),
			Service:    NewMockService(ctrl),
		}

		mockWorktree.
			EXPECT().
			DetachHead(gomock.Any(), "main").
			Return(nil)

		err := handler.CheckoutBranch(t.Context(), &Request{
			Branch:  "main",
			Options: &Options{Detach: true},
		})
		assert.NoError(t, err)
	})
}

func TestHandler_CheckoutBranch_NonTrunk(t *testing.T) {
	mockStore := NewMockStore(gomock.NewController(t))
	mockStore.
		EXPECT().
		Trunk().
		Return("main").
		AnyTimes()

	t.Run("AlreadyRestacked", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockWorktree := NewMockGitWorktree(ctrl)
		mockService := NewMockService(ctrl)

		handler := &Handler{
			Stdout:     io.Discard,
			Log:        silog.Nop(),
			Store:      mockStore,
			Repository: NewMockGitRepository(ctrl),
			Worktree:   mockWorktree,
			Track:      NewMockTrackHandler(ctrl),
			Service:    mockService,
		}

		mockService.
			EXPECT().
			VerifyRestacked(gomock.Any(), "feature").
			Return(nil)
		mockWorktree.
			EXPECT().
			CheckoutBranch(gomock.Any(), "feature").
			Return(nil)

		err := handler.CheckoutBranch(t.Context(), &Request{
			Branch: "feature",
		})
		assert.NoError(t, err)
	})

	t.Run("DryRun", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockWorktree := NewMockGitWorktree(ctrl)
		mockTrack := NewMockTrackHandler(ctrl)
		mockService := NewMockService(ctrl)

		var stdout bytes.Buffer
		handler := &Handler{
			Stdout:     &stdout,
			Log:        silog.Nop(),
			Store:      mockStore,
			Repository: NewMockGitRepository(ctrl),
			Worktree:   mockWorktree,
			Track:      mockTrack,
			Service:    mockService,
		}

		mockService.
			EXPECT().
			VerifyRestacked(gomock.Any(), "feature").
			Return(nil)

		req := &Request{
			Branch:  "feature",
			Options: &Options{DryRun: true},
		}

		err := handler.CheckoutBranch(t.Context(), req)
		assert.NoError(t, err)
		assert.Equal(t, "feature\n", stdout.String())
	})

	t.Run("Detach", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockWorktree := NewMockGitWorktree(ctrl)
		mockService := NewMockService(ctrl)

		handler := &Handler{
			Stdout:     io.Discard,
			Log:        silog.Nop(),
			Store:      mockStore,
			Repository: NewMockGitRepository(ctrl),
			Worktree:   mockWorktree,
			Track:      NewMockTrackHandler(ctrl),
			Service:    mockService,
		}

		mockService.
			EXPECT().
			VerifyRestacked(gomock.Any(), "feature").
			Return(nil)
		mockWorktree.
			EXPECT().
			DetachHead(gomock.Any(), "feature").
			Return(nil)

		err := handler.CheckoutBranch(t.Context(), &Request{
			Branch:  "feature",
			Options: &Options{Detach: true},
		})
		assert.NoError(t, err)
	})

	t.Run("NeedsRestack", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockWorktree := NewMockGitWorktree(ctrl)
		mockService := NewMockService(ctrl)

		var logBuffer bytes.Buffer
		handler := &Handler{
			Stdout:     io.Discard,
			Log:        silog.New(&logBuffer, nil),
			Store:      mockStore,
			Repository: NewMockGitRepository(ctrl),
			Worktree:   mockWorktree,
			Track:      NewMockTrackHandler(ctrl),
			Service:    mockService,
		}

		mockService.
			EXPECT().
			VerifyRestacked(gomock.Any(), "feature").
			Return(&spice.BranchNeedsRestackError{
				Base:     "main",
				BaseHash: git.Hash("abc123"),
			})
		mockWorktree.
			EXPECT().
			CheckoutBranch(gomock.Any(), "feature").
			Return(nil)

		err := handler.CheckoutBranch(t.Context(), &Request{
			Branch: "feature",
		})
		assert.NoError(t, err)
		assert.Contains(t, logBuffer.String(), "needs to be restacked")
		assert.Contains(t, logBuffer.String(), "gs branch restack --branch=feature")
	})

	t.Run("NotTrackedButShouldTrack", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockWorktree := NewMockGitWorktree(ctrl)
		mockTrack := NewMockTrackHandler(ctrl)
		mockService := NewMockService(ctrl)

		handler := &Handler{
			Stdout:     io.Discard,
			Log:        silog.Nop(),
			Store:      mockStore,
			Repository: NewMockGitRepository(ctrl),
			Worktree:   mockWorktree,
			Track:      mockTrack,
			Service:    mockService,
		}

		mockService.
			EXPECT().
			VerifyRestacked(gomock.Any(), "feature").
			Return(state.ErrNotExist)
		mockTrack.
			EXPECT().
			AddBranch(gomock.Any(), &track.AddBranchRequest{
				Branch: "feature",
			}).
			Return(nil)
		mockWorktree.
			EXPECT().
			CheckoutBranch(gomock.Any(), "feature").
			Return(nil)

		err := handler.CheckoutBranch(t.Context(), &Request{
			Branch: "feature",
			ShouldTrack: func(string) (bool, error) {
				return true, nil
			},
		})
		assert.NoError(t, err)
	})

	t.Run("NotTrackedNotRequested", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockWorktree := NewMockGitWorktree(ctrl)
		mockService := NewMockService(ctrl)

		handler := &Handler{
			Stdout:     io.Discard,
			Log:        silog.Nop(),
			Store:      mockStore,
			Repository: NewMockGitRepository(ctrl),
			Worktree:   mockWorktree,
			Track:      NewMockTrackHandler(ctrl),
			Service:    mockService,
		}

		mockService.
			EXPECT().
			VerifyRestacked(gomock.Any(), "feature").
			Return(state.ErrNotExist)
		mockWorktree.
			EXPECT().
			CheckoutBranch(gomock.Any(), "feature").
			Return(nil)

		err := handler.CheckoutBranch(t.Context(), &Request{
			Branch: "feature",
		})
		assert.NoError(t, err)
	})

	t.Run("BranchDoesNotExist", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockService := NewMockService(ctrl)

		handler := &Handler{
			Stdout:     io.Discard,
			Log:        silog.Nop(),
			Store:      mockStore,
			Repository: NewMockGitRepository(ctrl),
			Worktree:   NewMockGitWorktree(ctrl),
			Track:      NewMockTrackHandler(ctrl),
			Service:    mockService,
		}

		mockService.
			EXPECT().
			VerifyRestacked(gomock.Any(), "feature").
			Return(git.ErrNotExist)
		mockStore.
			EXPECT().
			Remote().
			Return("", git.ErrNotExist)

		err := handler.CheckoutBranch(t.Context(), &Request{
			Branch: "feature",
		})
		assert.Error(t, err)
		assert.ErrorContains(t, err, `branch "feature" does not exist`)
	})

	t.Run("RemoteBranchExists", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockRepo := NewMockGitRepository(ctrl)
		mockService := NewMockService(ctrl)
		mockWorktree := NewMockGitWorktree(ctrl)

		var logBuffer bytes.Buffer
		handler := &Handler{
			Stdout:     io.Discard,
			Log:        silog.New(&logBuffer, nil),
			Store:      mockStore,
			Repository: mockRepo,
			Worktree:   mockWorktree,
			Track:      NewMockTrackHandler(ctrl),
			Service:    mockService,
		}

		mockService.
			EXPECT().
			VerifyRestacked(gomock.Any(), "feature").
			Return(git.ErrNotExist)
		mockStore.
			EXPECT().
			Remote().
			Return("origin", nil)
		mockRepo.
			EXPECT().
			PeelToCommit(gomock.Any(), "origin/feature").
			Return(git.Hash("abc123"), nil)
		mockRepo.
			EXPECT().
			CreateBranch(gomock.Any(), git.CreateBranchRequest{
				Name: "feature",
				Head: "abc123",
			}).
			Return(nil)
		mockRepo.
			EXPECT().
			SetBranchUpstream(gomock.Any(), "feature", "origin/feature").
			Return(nil)
		mockWorktree.
			EXPECT().
			CheckoutBranch(gomock.Any(), "feature").
			Return(nil)

		err := handler.CheckoutBranch(t.Context(), &Request{
			Branch: "feature",
		})
		assert.NoError(t, err)
		assert.Contains(t, logBuffer.String(), "found remote branch origin/feature")
	})

	t.Run("RemoteBranchExistsNoUpstream", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockRepo := NewMockGitRepository(ctrl)
		mockService := NewMockService(ctrl)
		mockWorktree := NewMockGitWorktree(ctrl)

		var logBuffer bytes.Buffer
		handler := &Handler{
			Stdout:     io.Discard,
			Log:        silog.New(&logBuffer, nil),
			Store:      mockStore,
			Repository: mockRepo,
			Worktree:   mockWorktree,
			Track:      NewMockTrackHandler(ctrl),
			Service:    mockService,
		}

		upstreamError := errors.New("upstream error")
		mockService.
			EXPECT().
			VerifyRestacked(gomock.Any(), "feature").
			Return(git.ErrNotExist)
		mockStore.
			EXPECT().
			Remote().
			Return("origin", nil)
		mockRepo.
			EXPECT().
			PeelToCommit(gomock.Any(), "origin/feature").
			Return(git.Hash("abc123"), nil)
		mockRepo.
			EXPECT().
			CreateBranch(gomock.Any(), git.CreateBranchRequest{
				Name: "feature",
				Head: "abc123",
			}).
			Return(nil)
		mockRepo.
			EXPECT().
			SetBranchUpstream(gomock.Any(), "feature", "origin/feature").
			Return(upstreamError)
		mockWorktree.
			EXPECT().
			CheckoutBranch(gomock.Any(), "feature").
			Return(nil)

		err := handler.CheckoutBranch(t.Context(), &Request{
			Branch: "feature",
		})
		assert.NoError(t, err)
		assert.Contains(t, logBuffer.String(), "found remote branch origin/feature")
		assert.Contains(t, logBuffer.String(), "Error setting upstream")
	})

	t.Run("RemoteBranchCreateFails", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockRepo := NewMockGitRepository(ctrl)
		mockService := NewMockService(ctrl)

		handler := &Handler{
			Stdout:     io.Discard,
			Log:        silog.Nop(),
			Store:      mockStore,
			Repository: mockRepo,
			Worktree:   NewMockGitWorktree(ctrl),
			Track:      NewMockTrackHandler(ctrl),
			Service:    mockService,
		}

		createError := errors.New("create branch failed")
		mockService.
			EXPECT().
			VerifyRestacked(gomock.Any(), "feature").
			Return(git.ErrNotExist)
		mockStore.
			EXPECT().
			Remote().
			Return("origin", nil)
		mockRepo.
			EXPECT().
			PeelToCommit(gomock.Any(), "origin/feature").
			Return(git.Hash("abc123"), nil)
		mockRepo.
			EXPECT().
			CreateBranch(gomock.Any(), git.CreateBranchRequest{
				Name: "feature",
				Head: "abc123",
			}).
			Return(createError)

		err := handler.CheckoutBranch(t.Context(), &Request{
			Branch: "feature",
		})
		assert.Error(t, err)
		assert.ErrorContains(t, err, "create branch from remote")
		assert.ErrorIs(t, err, createError)
	})

	t.Run("RemoteBranchNotFound", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockRepo := NewMockGitRepository(ctrl)
		mockService := NewMockService(ctrl)

		handler := &Handler{
			Stdout:     io.Discard,
			Log:        silog.Nop(),
			Store:      mockStore,
			Repository: mockRepo,
			Worktree:   NewMockGitWorktree(ctrl),
			Track:      NewMockTrackHandler(ctrl),
			Service:    mockService,
		}

		mockService.
			EXPECT().
			VerifyRestacked(gomock.Any(), "feature").
			Return(git.ErrNotExist)
		mockStore.
			EXPECT().
			Remote().
			Return("origin", nil)
		mockRepo.
			EXPECT().
			PeelToCommit(gomock.Any(), "origin/feature").
			Return(git.Hash(""), git.ErrNotExist)

		err := handler.CheckoutBranch(t.Context(), &Request{
			Branch: "feature",
		})
		assert.Error(t, err)
		assert.ErrorContains(t, err, `branch "feature" does not exist`)
	})

	t.Run("OtherVerifyError", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockWorktree := NewMockGitWorktree(ctrl)
		mockService := NewMockService(ctrl)

		var logBuffer bytes.Buffer
		handler := &Handler{
			Stdout:     io.Discard,
			Log:        silog.New(&logBuffer, nil),
			Store:      mockStore,
			Repository: NewMockGitRepository(ctrl),
			Worktree:   mockWorktree,
			Track:      NewMockTrackHandler(ctrl),
			Service:    mockService,
		}

		unknownError := errors.New("some other error")
		mockService.
			EXPECT().
			VerifyRestacked(gomock.Any(), "feature").
			Return(unknownError)
		mockWorktree.
			EXPECT().
			CheckoutBranch(gomock.Any(), "feature").
			Return(nil)

		err := handler.CheckoutBranch(t.Context(), &Request{
			Branch: "feature",
		})
		assert.NoError(t, err)
		assert.Contains(t, logBuffer.String(), "Unable to check if branch is restacked")
	})
}

func TestHandler_CheckoutBranch_EdgeCases(t *testing.T) {
	mockStore := NewMockStore(gomock.NewController(t))
	mockStore.
		EXPECT().
		Trunk().
		Return("main").
		AnyTimes()

	t.Run("ShouldTrackError", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockService := NewMockService(ctrl)

		handler := &Handler{
			Stdout:     io.Discard,
			Log:        silog.Nop(),
			Store:      mockStore,
			Repository: NewMockGitRepository(ctrl),
			Worktree:   NewMockGitWorktree(ctrl),
			Track:      NewMockTrackHandler(ctrl),
			Service:    mockService,
		}

		mockService.
			EXPECT().
			VerifyRestacked(gomock.Any(), "feature").
			Return(state.ErrNotExist)

		shouldTrackError := errors.New("should track error")
		err := handler.CheckoutBranch(t.Context(), &Request{
			Branch: "feature",
			ShouldTrack: func(string) (bool, error) {
				return false, shouldTrackError
			},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "check if branch should be tracked")
		assert.ErrorIs(t, err, shouldTrackError)
	})

	t.Run("TrackError", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockTrack := NewMockTrackHandler(ctrl)
		mockService := NewMockService(ctrl)
		mockWorktree := NewMockGitWorktree(ctrl)

		var logBuffer bytes.Buffer
		handler := &Handler{
			Stdout:     io.Discard,
			Log:        silog.New(&logBuffer, nil),
			Store:      mockStore,
			Repository: NewMockGitRepository(ctrl),
			Worktree:   mockWorktree,
			Track:      mockTrack,
			Service:    mockService,
		}

		trackError := errors.New("track error")
		mockService.
			EXPECT().
			VerifyRestacked(gomock.Any(), "feature").
			Return(state.ErrNotExist)
		mockTrack.
			EXPECT().
			AddBranch(gomock.Any(), &track.AddBranchRequest{
				Branch: "feature",
			}).
			Return(trackError)
		mockWorktree.
			EXPECT().
			CheckoutBranch(gomock.Any(), "feature").
			Return(nil)

		err := handler.CheckoutBranch(t.Context(), &Request{
			Branch: "feature",
			ShouldTrack: func(string) (bool, error) {
				return true, nil
			},
		})
		require.NoError(t, err)
		assert.Contains(t, logBuffer.String(), "Error tracking branch")
	})

	t.Run("CheckoutError", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockWorktree := NewMockGitWorktree(ctrl)
		mockService := NewMockService(ctrl)

		handler := &Handler{
			Stdout:     io.Discard,
			Log:        silog.Nop(),
			Store:      mockStore,
			Repository: NewMockGitRepository(ctrl),
			Worktree:   mockWorktree,
			Track:      NewMockTrackHandler(ctrl),
			Service:    mockService,
		}

		checkoutError := errors.New("checkout error")
		mockService.
			EXPECT().
			VerifyRestacked(gomock.Any(), "feature").
			Return(nil)
		mockWorktree.
			EXPECT().
			CheckoutBranch(gomock.Any(), "feature").
			Return(checkoutError)

		err := handler.CheckoutBranch(t.Context(), &Request{
			Branch: "feature",
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "checkout branch")
		assert.ErrorIs(t, err, checkoutError)
	})

	t.Run("DetachError", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockWorktree := NewMockGitWorktree(ctrl)
		mockService := NewMockService(ctrl)

		handler := &Handler{
			Stdout:     io.Discard,
			Log:        silog.Nop(),
			Store:      mockStore,
			Repository: NewMockGitRepository(ctrl),
			Worktree:   mockWorktree,
			Track:      NewMockTrackHandler(ctrl),
			Service:    mockService,
		}

		detachError := errors.New("detach error")
		mockService.
			EXPECT().
			VerifyRestacked(gomock.Any(), "feature").
			Return(nil)
		mockWorktree.
			EXPECT().
			DetachHead(gomock.Any(), "feature").
			Return(detachError)

		err := handler.CheckoutBranch(t.Context(), &Request{
			Branch:  "feature",
			Options: &Options{Detach: true},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "detach HEAD")
		assert.ErrorIs(t, err, detachError)
	})
}

func TestHandler_CheckoutBranch_Verbose(t *testing.T) {
	mockStore := NewMockStore(gomock.NewController(t))
	mockStore.
		EXPECT().
		Trunk().
		Return("main").
		AnyTimes()

	t.Run("VerboseTrue", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockWorktree := NewMockGitWorktree(ctrl)

		var logBuffer bytes.Buffer
		handler := &Handler{
			Stdout:     io.Discard,
			Log:        silog.New(&logBuffer, nil),
			Store:      mockStore,
			Repository: NewMockGitRepository(ctrl),
			Worktree:   mockWorktree,
			Track:      NewMockTrackHandler(ctrl),
			Service:    NewMockService(ctrl),
		}

		mockWorktree.
			EXPECT().
			CheckoutBranch(gomock.Any(), "main").
			Return(nil)

		err := handler.CheckoutBranch(t.Context(), &Request{
			Branch:  "main",
			Options: &Options{Verbose: true},
		})
		assert.NoError(t, err)
		assert.Contains(t, logBuffer.String(), "switched to branch: main")
	})

	t.Run("VerboseFalse", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockWorktree := NewMockGitWorktree(ctrl)

		var logBuffer bytes.Buffer
		handler := &Handler{
			Stdout:     io.Discard,
			Log:        silog.New(&logBuffer, nil),
			Store:      mockStore,
			Repository: NewMockGitRepository(ctrl),
			Worktree:   mockWorktree,
			Track:      NewMockTrackHandler(ctrl),
			Service:    NewMockService(ctrl),
		}

		mockWorktree.
			EXPECT().
			CheckoutBranch(gomock.Any(), "main").
			Return(nil)

		err := handler.CheckoutBranch(t.Context(), &Request{
			Branch:  "main",
			Options: &Options{Verbose: false},
		})
		assert.NoError(t, err)
		assert.NotContains(t, logBuffer.String(), "switched to branch")
	})

	t.Run("DryRunNoVerbose", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		var logBuffer bytes.Buffer
		var stdout bytes.Buffer
		handler := &Handler{
			Stdout:     &stdout,
			Log:        silog.New(&logBuffer, nil),
			Store:      mockStore,
			Repository: NewMockGitRepository(ctrl),
			Worktree:   NewMockGitWorktree(ctrl),
			Track:      NewMockTrackHandler(ctrl),
			Service:    NewMockService(ctrl),
		}

		err := handler.CheckoutBranch(t.Context(), &Request{
			Branch:  "main",
			Options: &Options{DryRun: true, Verbose: true},
		})
		assert.NoError(t, err)
		assert.Equal(t, "main\n", stdout.String())
		assert.NotContains(t, logBuffer.String(), "switched to branch")
	})
}
