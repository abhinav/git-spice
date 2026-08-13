package restack

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state/statetest"
)

func TestHandler_RestackDownstack(t *testing.T) {
	t.Run("SuccessfulRestack", func(t *testing.T) {
		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, nil)
		ctrl := gomock.NewController(t)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			BranchGraph(gomock.Any(), gomock.Any()).
			Return(newBranchGraphBuilder("main").
				Branch("feature1", "main").
				Branch("feature2", "feature1").
				Branch("feature3", "feature2").
				Branch("feature4", "feature3").
				Build(t), nil)
		mockService.EXPECT().
			Restack(gomock.Any(), "feature1").
			Return(&spice.RestackResponse{Base: "main"}, nil)
		mockService.EXPECT().
			Restack(gomock.Any(), "feature2").
			Return(&spice.RestackResponse{Base: "feature1"}, nil)
		mockService.EXPECT().
			Restack(gomock.Any(), "feature3").
			Return(&spice.RestackResponse{Base: "feature2"}, nil)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			RootDir().
			Return(t.TempDir())
		mockWorktree.EXPECT().
			CheckoutBranch(gomock.Any(), "feature3").
			Return(nil)

		handler := &Handler{
			Log:      log,
			Worktree: mockWorktree,
			Store:    statetest.NewMemoryStore(t, "main", "", log),
			Service:  mockService,
		}

		require.NoError(t, handler.RestackDownstack(t.Context(), &DownstackRequest{
			Branch: "feature3",
		}))
		assert.Contains(t, logBuffer.String(), "feature1: restacked on main")
		assert.Contains(t, logBuffer.String(), "feature2: restacked on feature1")
		assert.Contains(t, logBuffer.String(), "feature3: restacked on feature2")
		assert.NotContains(t, logBuffer.String(), "feature4: restacked")
	})

	t.Run("RestackInterrupted", func(t *testing.T) {
		log := silog.Nop()
		ctrl := gomock.NewController(t)

		rebaseErr := &git.RebaseInterruptError{
			Kind: git.RebaseInterruptConflict,
		}

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			BranchGraph(gomock.Any(), gomock.Any()).
			Return(newBranchGraphBuilder("main").
				Branch("feature1", "main").
				Branch("feature2", "feature1").
				Build(t), nil)
		mockService.EXPECT().
			Restack(gomock.Any(), "feature1").
			Return(nil, rebaseErr)
		mockService.EXPECT().
			RebaseRescue(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ any, req spice.RebaseRescueRequest) error {
				assert.Equal(t, []string{"downstack", "restack"}, req.Command)
				assert.Equal(t, "feature2", req.Branch)
				return nil
			})

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			RootDir().
			Return(t.TempDir())

		handler := &Handler{
			Log:      log,
			Worktree: mockWorktree,
			Store:    statetest.NewMemoryStore(t, "main", "", log),
			Service:  mockService,
		}

		require.NoError(t, handler.RestackDownstack(t.Context(), &DownstackRequest{
			Branch: "feature2",
		}))
	})
}
