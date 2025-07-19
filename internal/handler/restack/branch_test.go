package restack

import (
	"bytes"
	"context"
	"errors"
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

func TestHandler_RestackBranch(t *testing.T) {
	t.Run("SuccessfulRestack", func(t *testing.T) {
		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, nil)
		ctrl := gomock.NewController(t)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			Restack(gomock.Any(), "feature").
			Return(&spice.RestackResponse{Base: "main"}, nil)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			Checkout(gomock.Any(), "feature").
			Return(nil)

		handler := &Handler{
			Log:      log,
			Worktree: mockWorktree,
			Store:    statetest.NewMemoryStore(t, "main", "", log),
			Service:  mockService,
		}
		require.NoError(t, handler.RestackBranch(context.Background(), "feature"))
		assert.Contains(t, logBuffer.String(), "feature: restacked on main")
	})

	t.Run("RestackInterrupted", func(t *testing.T) {
		log := silog.Nop()
		ctrl := gomock.NewController(t)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			Restack(gomock.Any(), "feature").
			Return(nil, &git.RebaseInterruptError{
				Kind: git.RebaseInterruptConflict,
			})
		mockService.EXPECT().
			RebaseRescue(gomock.Any(), gomock.Any()).
			Return(nil)

		handler := &Handler{
			Log:      log,
			Worktree: NewMockGitWorktree(ctrl),
			Store:    statetest.NewMemoryStore(t, "main", "", log),
			Service:  mockService,
		}

		require.NoError(t, handler.RestackBranch(context.Background(), "feature"))
	})

	t.Run("UntrackedBranch", func(t *testing.T) {
		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, nil)

		ctrl := gomock.NewController(t)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			Restack(gomock.Any(), "untracked").
			Return(nil, state.ErrNotExist)

		handler := &Handler{
			Log:      log,
			Worktree: NewMockGitWorktree(ctrl),
			Store:    statetest.NewMemoryStore(t, "main", "", log),
			Service:  mockService,
		}

		err := handler.RestackBranch(context.Background(), "untracked")
		require.Error(t, err)
		assert.ErrorContains(t, err, "untracked branch")
		assert.Contains(t, logBuffer.String(), "untracked: branch not tracked: run 'gs branch track")
	})

	t.Run("AlreadyRestacked", func(t *testing.T) {
		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, nil)

		ctrl := gomock.NewController(t)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			Restack(gomock.Any(), "already-restacked").
			Return(nil, spice.ErrAlreadyRestacked)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			Checkout(gomock.Any(), "already-restacked").
			Return(nil)

		handler := &Handler{
			Log:      log,
			Worktree: mockWorktree,
			Store:    statetest.NewMemoryStore(t, "main", "", log),
			Service:  mockService,
		}
		require.NoError(t, handler.RestackBranch(context.Background(), "already-restacked"))
		assert.Contains(t, logBuffer.String(), "already-restacked: branch does not need to be restacked.")
	})

	t.Run("UnexpectedError", func(t *testing.T) {
		log := silog.Nop()

		unexpectedErr := errors.New("unexpected error")

		ctrl := gomock.NewController(t)
		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			Restack(gomock.Any(), "feature").
			Return(nil, unexpectedErr)

		handler := &Handler{
			Log:      log,
			Worktree: NewMockGitWorktree(ctrl),
			Store:    statetest.NewMemoryStore(t, "main", "", log),
			Service:  mockService,
		}
		err := handler.RestackBranch(context.Background(), "feature")
		require.Error(t, err)
		assert.ErrorContains(t, err, "restack branch")
		assert.ErrorIs(t, err, unexpectedErr)
	})
}
