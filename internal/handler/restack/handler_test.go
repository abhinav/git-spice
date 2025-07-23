package restack

import (
	"bytes"
	"context"
	"errors"
	"slices"
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

func TestHandler_Restack(t *testing.T) {
	t.Run("ScopeBranch", func(t *testing.T) {
		t.Run("SuccessfulRestack", func(t *testing.T) {
			var logBuffer bytes.Buffer
			log := silog.New(&logBuffer, nil)
			ctrl := gomock.NewController(t)

			mockService := NewMockService(ctrl)
			mockService.EXPECT().
				BranchGraph(gomock.Any(), gomock.Any()).
				Return(newBranchGraphBuilder("main").
					Branch("feature", "main").
					Build(t), nil)
			mockService.EXPECT().
				Restack(gomock.Any(), "feature").
				Return(&spice.RestackResponse{Base: "main"}, nil)

			mockWorktree := NewMockGitWorktree(ctrl)
			mockWorktree.EXPECT().
				RootDir().
				Return(t.TempDir())
			mockWorktree.EXPECT().
				Checkout(gomock.Any(), "feature").
				Return(nil)

			handler := &Handler{
				Log:      log,
				Worktree: mockWorktree,
				Store:    statetest.NewMemoryStore(t, "main", "", log),
				Service:  mockService,
			}

			count, err := handler.Restack(t.Context(), &Request{
				Branch:          "feature",
				ContinueCommand: []string{"false"},
			})

			require.NoError(t, err)
			assert.Equal(t, 1, count)
			assert.Contains(t, logBuffer.String(), "feature: restacked on main")
		})

		t.Run("TrunkBranch", func(t *testing.T) {
			log := silog.Nop()
			ctrl := gomock.NewController(t)

			mockService := NewMockService(ctrl)
			mockService.EXPECT().
				BranchGraph(gomock.Any(), gomock.Any()).
				Return(newBranchGraphBuilder("main").
					Branch("feature", "main").
					Build(t), nil)

			mockWorktree := NewMockGitWorktree(ctrl)
			handler := &Handler{
				Log:      log,
				Worktree: mockWorktree,
				Store:    statetest.NewMemoryStore(t, "main", "", log),
				Service:  mockService,
			}

			_, err := handler.Restack(t.Context(), &Request{
				Branch:          "main",
				ContinueCommand: []string{"false"},
				Scope:           ScopeBranch,
			})

			require.Error(t, err)
			assert.ErrorContains(t, err, "trunk cannot be restacked")
		})
	})

	t.Run("ScopeUpstack", func(t *testing.T) {
		t.Run("SuccessfulRestack", func(t *testing.T) {
			var logBuffer bytes.Buffer
			log := silog.New(&logBuffer, nil)
			ctrl := gomock.NewController(t)

			mockService := NewMockService(ctrl)
			mockService.EXPECT().
				BranchGraph(gomock.Any(), gomock.Any()).
				Return(newBranchGraphBuilder("main").
					Branch("feature", "main").
					Branch("feature2", "feature").
					Branch("feature3", "feature2").
					Build(t), nil)

			mockService.EXPECT().
				Restack(gomock.Any(), "feature").
				Return(&spice.RestackResponse{Base: "main"}, nil)
			mockService.EXPECT().
				Restack(gomock.Any(), "feature2").
				Return(&spice.RestackResponse{Base: "feature"}, nil)
			mockService.EXPECT().
				Restack(gomock.Any(), "feature3").
				Return(&spice.RestackResponse{Base: "feature2"}, nil)

			mockWorktree := NewMockGitWorktree(ctrl)
			mockWorktree.EXPECT().
				RootDir().
				Return(t.TempDir())
			mockWorktree.EXPECT().
				Checkout(gomock.Any(), "feature").
				Return(nil)

			handler := &Handler{
				Log:      log,
				Worktree: mockWorktree,
				Store:    statetest.NewMemoryStore(t, "main", "", log),
				Service:  mockService,
			}

			count, err := handler.Restack(t.Context(), &Request{
				Branch:          "feature",
				ContinueCommand: []string{"false"},
				Scope:           ScopeUpstack,
			})

			require.NoError(t, err)
			assert.Equal(t, 3, count)
			assert.Contains(t, logBuffer.String(), "feature: restacked on main")
			assert.Contains(t, logBuffer.String(), "feature2: restacked on feature")
			assert.Contains(t, logBuffer.String(), "feature3: restacked on feature2")
		})

		t.Run("UpstackExclusiveOnly", func(t *testing.T) {
			var logBuffer bytes.Buffer
			log := silog.New(&logBuffer, nil)
			ctrl := gomock.NewController(t)

			mockService := NewMockService(ctrl)
			mockService.EXPECT().
				BranchGraph(gomock.Any(), gomock.Any()).
				Return(newBranchGraphBuilder("main").
					Branch("feature", "main").
					Branch("feature2", "feature").
					Branch("feature3", "feature2").
					Build(t), nil)
			mockService.EXPECT().
				Restack(gomock.Any(), "feature2").
				Return(&spice.RestackResponse{Base: "feature"}, nil)
			mockService.EXPECT().
				Restack(gomock.Any(), "feature3").
				Return(&spice.RestackResponse{Base: "feature2"}, nil)

			mockWorktree := NewMockGitWorktree(ctrl)
			mockWorktree.EXPECT().
				RootDir().
				Return(t.TempDir())
			mockWorktree.EXPECT().
				Checkout(gomock.Any(), "feature").
				Return(nil)

			handler := &Handler{
				Log:      log,
				Worktree: mockWorktree,
				Store:    statetest.NewMemoryStore(t, "main", "", log),
				Service:  mockService,
			}

			count, err := handler.Restack(t.Context(), &Request{
				Branch:          "feature",
				ContinueCommand: []string{"false"},
				Scope:           ScopeUpstackExclusive,
			})

			require.NoError(t, err)
			assert.Equal(t, 2, count)
			assert.NotContains(t, logBuffer.String(), "feature: restacked")
			assert.Contains(t, logBuffer.String(), "feature2: restacked on feature")
			assert.Contains(t, logBuffer.String(), "feature3: restacked on feature2")
		})

		t.Run("EmptyUpstack", func(t *testing.T) {
			log := silog.Nop()
			ctrl := gomock.NewController(t)

			mockService := NewMockService(ctrl)
			mockService.EXPECT().
				BranchGraph(gomock.Any(), gomock.Any()).
				Return(newBranchGraphBuilder("main").
					Branch("feature", "main").
					Build(t), nil)
			mockService.EXPECT().
				Restack(gomock.Any(), "feature").
				Return(&spice.RestackResponse{Base: "main"}, nil)

			mockWorktree := NewMockGitWorktree(ctrl)
			mockWorktree.EXPECT().
				RootDir().
				Return(t.TempDir())
			mockWorktree.EXPECT().
				Checkout(gomock.Any(), "feature").
				Return(nil)

			handler := &Handler{
				Log:      log,
				Worktree: mockWorktree,
				Store:    statetest.NewMemoryStore(t, "main", "", log),
				Service:  mockService,
			}

			count, err := handler.Restack(t.Context(), &Request{
				Branch:          "feature",
				ContinueCommand: []string{"false"},
				Scope:           ScopeUpstack,
			})

			require.NoError(t, err)
			assert.Equal(t, 1, count)
		})
	})

	t.Run("ScopeDownstack", func(t *testing.T) {
		t.Run("SuccessfulRestack", func(t *testing.T) {
			var logBuffer bytes.Buffer
			log := silog.New(&logBuffer, nil)
			ctrl := gomock.NewController(t)

			mockService := NewMockService(ctrl)
			mockService.EXPECT().
				BranchGraph(gomock.Any(), gomock.Any()).
				Return(newBranchGraphBuilder("main").
					Branch("base1", "main").
					Branch("base2", "base1").
					Branch("feature", "base2").
					Build(t), nil)
			mockService.EXPECT().
				Restack(gomock.Any(), "base1").
				Return(&spice.RestackResponse{Base: "main"}, nil)
			mockService.EXPECT().
				Restack(gomock.Any(), "base2").
				Return(&spice.RestackResponse{Base: "base1"}, nil)
			mockService.EXPECT().
				Restack(gomock.Any(), "feature").
				Return(&spice.RestackResponse{Base: "base2"}, nil)

			mockWorktree := NewMockGitWorktree(ctrl)
			mockWorktree.EXPECT().
				RootDir().
				Return(t.TempDir())
			mockWorktree.EXPECT().
				Checkout(gomock.Any(), "feature").
				Return(nil)

			handler := &Handler{
				Log:      log,
				Worktree: mockWorktree,
				Store:    statetest.NewMemoryStore(t, "main", "", log),
				Service:  mockService,
			}

			count, err := handler.Restack(t.Context(), &Request{
				Branch:          "feature",
				ContinueCommand: []string{"false"},
				Scope:           ScopeDownstack,
			})

			require.NoError(t, err)
			assert.Equal(t, 3, count)
			assert.Contains(t, logBuffer.String(), "base1: restacked on main")
			assert.Contains(t, logBuffer.String(), "base2: restacked on base1")
			assert.Contains(t, logBuffer.String(), "feature: restacked on base2")
		})

		t.Run("EmptyDownstack", func(t *testing.T) {
			log := silog.Nop()
			ctrl := gomock.NewController(t)

			mockService := NewMockService(ctrl)
			mockService.EXPECT().
				BranchGraph(gomock.Any(), gomock.Any()).
				Return(newBranchGraphBuilder("main").
					Branch("feature", "main").
					Build(t), nil)
			mockService.EXPECT().
				Restack(gomock.Any(), "feature").
				Return(&spice.RestackResponse{Base: "main"}, nil)

			mockWorktree := NewMockGitWorktree(ctrl)
			mockWorktree.EXPECT().
				RootDir().
				Return(t.TempDir())
			mockWorktree.EXPECT().
				Checkout(gomock.Any(), "feature").
				Return(nil)

			handler := &Handler{
				Log:      log,
				Worktree: mockWorktree,
				Store:    statetest.NewMemoryStore(t, "main", "", log),
				Service:  mockService,
			}

			count, err := handler.Restack(t.Context(), &Request{
				Branch:          "feature",
				ContinueCommand: []string{"false"},
				Scope:           ScopeDownstack,
			})

			require.NoError(t, err)
			assert.Equal(t, 1, count)
		})
	})

	t.Run("ScopeStack", func(t *testing.T) {
		t.Run("SuccessfulRestack", func(t *testing.T) {
			var logBuffer bytes.Buffer
			log := silog.New(&logBuffer, nil)
			ctrl := gomock.NewController(t)

			mockService := NewMockService(ctrl)
			mockService.EXPECT().
				BranchGraph(gomock.Any(), gomock.Any()).
				Return(newBranchGraphBuilder("main").
					Branch("base1", "main").
					Branch("base2", "base1").
					Branch("feature", "base2").
					Branch("feature2", "feature").
					Branch("feature3", "feature2").
					Build(t), nil)
			mockService.EXPECT().
				Restack(gomock.Any(), "base1").
				Return(&spice.RestackResponse{Base: "main"}, nil)
			mockService.EXPECT().
				Restack(gomock.Any(), "base2").
				Return(&spice.RestackResponse{Base: "base1"}, nil)
			mockService.EXPECT().
				Restack(gomock.Any(), "feature").
				Return(&spice.RestackResponse{Base: "base2"}, nil)
			mockService.EXPECT().
				Restack(gomock.Any(), "feature2").
				Return(&spice.RestackResponse{Base: "feature"}, nil)
			mockService.EXPECT().
				Restack(gomock.Any(), "feature3").
				Return(&spice.RestackResponse{Base: "feature2"}, nil)

			mockWorktree := NewMockGitWorktree(ctrl)
			mockWorktree.EXPECT().
				RootDir().
				Return(t.TempDir())
			mockWorktree.EXPECT().
				Checkout(gomock.Any(), "feature").
				Return(nil)

			handler := &Handler{
				Log:      log,
				Worktree: mockWorktree,
				Store:    statetest.NewMemoryStore(t, "main", "", log),
				Service:  mockService,
			}

			count, err := handler.Restack(t.Context(), &Request{
				Branch:          "feature",
				ContinueCommand: []string{"false"},
				Scope:           ScopeStack,
			})

			require.NoError(t, err)
			assert.Equal(t, 5, count)
			assert.Contains(t, logBuffer.String(), "base1: restacked on main")
			assert.Contains(t, logBuffer.String(), "base2: restacked on base1")
			assert.Contains(t, logBuffer.String(), "feature: restacked on base2")
			assert.Contains(t, logBuffer.String(), "feature2: restacked on feature")
			assert.Contains(t, logBuffer.String(), "feature3: restacked on feature2")
		})
	})

	t.Run("AlreadyRestacked", func(t *testing.T) {
		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, nil)
		ctrl := gomock.NewController(t)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			BranchGraph(gomock.Any(), gomock.Any()).
			Return(newBranchGraphBuilder("main").
				Branch("feature", "main").
				Branch("feature2", "feature").
				Build(t), nil)
		mockService.EXPECT().
			Restack(gomock.Any(), "feature").
			Return(nil, spice.ErrAlreadyRestacked)
		mockService.EXPECT().
			Restack(gomock.Any(), "feature2").
			Return(&spice.RestackResponse{Base: "feature"}, nil)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			RootDir().
			Return(t.TempDir())
		mockWorktree.EXPECT().
			Checkout(gomock.Any(), "feature").
			Return(nil)

		handler := &Handler{
			Log:      log,
			Worktree: mockWorktree,
			Store:    statetest.NewMemoryStore(t, "main", "", log),
			Service:  mockService,
		}

		count, err := handler.Restack(t.Context(), &Request{
			Branch:          "feature",
			ContinueCommand: []string{"false"},
			Scope:           ScopeUpstack,
		})

		require.NoError(t, err)
		assert.Equal(t, 1, count)
		assert.Contains(t, logBuffer.String(), "feature: branch does not need to be restacked")
		assert.Contains(t, logBuffer.String(), "feature2: restacked on feature")
	})

	t.Run("RebaseInterrupt", func(t *testing.T) {
		log := silog.Nop()
		ctrl := gomock.NewController(t)

		rebaseErr := &git.RebaseInterruptError{
			Kind: git.RebaseInterruptConflict,
		}

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			BranchGraph(gomock.Any(), gomock.Any()).
			Return(newBranchGraphBuilder("main").
				Branch("feature", "main").
				Build(t), nil)
		mockService.EXPECT().
			Restack(gomock.Any(), "feature").
			Return(nil, rebaseErr)
		mockService.EXPECT().
			RebaseRescue(gomock.Any(), gomock.Any()).
			Return(nil)

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

		count, err := handler.Restack(t.Context(), &Request{
			Branch:          "feature",
			ContinueCommand: []string{"false"},
			Scope:           ScopeBranch,
		})
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})
}

func TestHandler_Restack_trunk(t *testing.T) {
	t.Run("ScopeStackFromTrunk", func(t *testing.T) {
		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, nil)
		ctrl := gomock.NewController(t)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			BranchGraph(gomock.Any(), gomock.Any()).
			Return(newBranchGraphBuilder("main").
				Branch("feature1", "main").
				Branch("feature2", "feature1").
				Build(t), nil)
		mockService.EXPECT().
			Restack(gomock.Any(), "feature1").
			Return(&spice.RestackResponse{Base: "main"}, nil)
		mockService.EXPECT().
			Restack(gomock.Any(), "feature2").
			Return(&spice.RestackResponse{Base: "feature1"}, nil)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			RootDir().
			Return(t.TempDir())
		mockWorktree.EXPECT().
			Checkout(gomock.Any(), "main").
			Return(nil)

		handler := &Handler{
			Log:      log,
			Worktree: mockWorktree,
			Store:    statetest.NewMemoryStore(t, "main", "", log),
			Service:  mockService,
		}

		count, err := handler.Restack(t.Context(), &Request{
			Branch:          "main",
			ContinueCommand: []string{"false"},
			Scope:           ScopeStack,
		})

		require.NoError(t, err)
		assert.Equal(t, 2, count)
		assert.NotContains(t, logBuffer.String(), "main: restacked")
		assert.Contains(t, logBuffer.String(), "feature1: restacked on main")
		assert.Contains(t, logBuffer.String(), "feature2: restacked on feature1")
	})

	t.Run("ScopeDownstackFromTrunk", func(t *testing.T) {
		log := silog.Nop()
		ctrl := gomock.NewController(t)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			BranchGraph(gomock.Any(), gomock.Any()).
			Return(newBranchGraphBuilder("main").
				Branch("feature1", "main").
				Build(t), nil)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			RootDir().
			Return(t.TempDir())
		mockWorktree.EXPECT().
			Checkout(gomock.Any(), "main").
			Return(nil)

		handler := &Handler{
			Log:      log,
			Worktree: mockWorktree,
			Store:    statetest.NewMemoryStore(t, "main", "", log),
			Service:  mockService,
		}

		_, err := handler.Restack(t.Context(), &Request{
			Branch:          "main",
			ContinueCommand: []string{"false"},
			Scope:           ScopeDownstack,
		})
		require.NoError(t, err)
	})

	t.Run("ScopeUpstackFromTrunk", func(t *testing.T) {
		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, nil)
		ctrl := gomock.NewController(t)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			BranchGraph(gomock.Any(), gomock.Any()).
			Return(newBranchGraphBuilder("main").
				Branch("feature1", "main").
				Branch("feature2", "feature1").
				Build(t), nil)
		mockService.EXPECT().
			Restack(gomock.Any(), "feature1").
			Return(&spice.RestackResponse{Base: "main"}, nil)
		mockService.EXPECT().
			Restack(gomock.Any(), "feature2").
			Return(&spice.RestackResponse{Base: "feature1"}, nil)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			RootDir().
			Return(t.TempDir())
		mockWorktree.EXPECT().
			Checkout(gomock.Any(), "main").
			Return(nil)

		handler := &Handler{
			Log:      log,
			Worktree: mockWorktree,
			Store:    statetest.NewMemoryStore(t, "main", "", log),
			Service:  mockService,
		}

		count, err := handler.Restack(t.Context(), &Request{
			Branch:          "main",
			ContinueCommand: []string{"false"},
			Scope:           ScopeUpstack,
		})

		require.NoError(t, err)
		assert.Equal(t, 2, count)
		assert.NotContains(t, logBuffer.String(), "main: restacked")
		assert.Contains(t, logBuffer.String(), "feature1: restacked on main")
		assert.Contains(t, logBuffer.String(), "feature2: restacked on feature1")
	})
}

func TestHandler_Restack_skipCheckedOut(t *testing.T) {
	t.Run("Branch", func(t *testing.T) {
		// Branch-scoped restack operation,
		// and branch is checked out in another worktree.

		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, nil)
		ctrl := gomock.NewController(t)

		featureWorktree := t.TempDir()

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			BranchGraph(gomock.Any(), gomock.Any()).
			Return(newBranchGraphBuilder("main").
				Branch("feature", "main").
				Worktree("feature", featureWorktree).
				Build(t), nil)

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

		count, err := handler.Restack(t.Context(), &Request{
			Branch:          "feature",
			ContinueCommand: []string{"false"},
			Scope:           ScopeBranch,
		})
		require.NoError(t, err)
		assert.Zero(t, count, "nothing should've been restacked")

		assert.Regexp(t, `checked out in another worktree \(.*\), skipping`, logBuffer.String())
		assert.Contains(t, logBuffer.String(), "not checking out here")
	})

	t.Run("Upstack", func(t *testing.T) {
		// Upstack-scoped rebase,
		// one of the upstack branches is
		// checked out in another worktree.
		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, nil)
		ctrl := gomock.NewController(t)

		feature2WT := t.TempDir()

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			BranchGraph(gomock.Any(), gomock.Any()).
			Return(newBranchGraphBuilder("main").
				Branch("feature1", "main").
				Branch("feature2", "feature1").
				Branch("feature3", "feature2").
				Worktree("feature2", feature2WT).
				Build(t), nil)
		mockService.EXPECT().
			Restack(gomock.Any(), "feature1").
			Return(&spice.RestackResponse{Base: "main"}, nil)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			RootDir().
			Return(t.TempDir())
		mockWorktree.EXPECT().
			Checkout(gomock.Any(), "feature1").
			Return(nil)

		handler := &Handler{
			Log:      log,
			Worktree: mockWorktree,
			Store:    statetest.NewMemoryStore(t, "main", "", log),
			Service:  mockService,
		}

		count, err := handler.Restack(t.Context(), &Request{
			Branch:          "feature1",
			ContinueCommand: []string{"false"},
			Scope:           ScopeUpstack,
		})
		require.NoError(t, err)
		assert.Equal(t, 1, count, "feature1 must have been restacked")

		assert.Contains(t, logBuffer.String(), "feature1: restacked on main")
		assert.Regexp(t, `feature2: checked out in another worktree \(.*\), skipping`, logBuffer.String())
		assert.Contains(t, logBuffer.String(), "feature3: base branch feature2 was not restacked")
	})
}

func TestHandler_Restack_errors(t *testing.T) {
	t.Run("BranchGraph", func(t *testing.T) {
		log := silog.Nop()
		ctrl := gomock.NewController(t)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			BranchGraph(gomock.Any(), gomock.Any()).
			Return(nil, errors.New("graph error"))

		handler := &Handler{
			Log:      log,
			Worktree: NewMockGitWorktree(ctrl),
			Store:    statetest.NewMemoryStore(t, "main", "", log),
			Service:  mockService,
		}

		count, err := handler.Restack(t.Context(), &Request{
			Branch:          "feature",
			ContinueCommand: []string{"false"},
			Scope:           ScopeUpstack,
		})

		require.Error(t, err)
		assert.Equal(t, 0, count)
		assert.ErrorContains(t, err, "load branch graph")
	})

	t.Run("UntrackedBranch", func(t *testing.T) {
		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, nil)
		ctrl := gomock.NewController(t)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			BranchGraph(gomock.Any(), gomock.Any()).
			Return(newBranchGraphBuilder("main").Build(t), nil)
		mockService.EXPECT().
			Restack(gomock.Any(), "untracked").
			Return(nil, state.ErrNotExist)

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

		count, err := handler.Restack(t.Context(), &Request{
			Branch:          "untracked",
			ContinueCommand: []string{"false"},
			Scope:           ScopeBranch,
		})

		require.Error(t, err)
		assert.Equal(t, 0, count)
		assert.ErrorContains(t, err, "untracked branch")
		assert.Contains(t, logBuffer.String(), "untracked: branch not tracked")
	})

	t.Run("UnexpectedRestackError", func(t *testing.T) {
		log := silog.Nop()
		ctrl := gomock.NewController(t)

		unexpectedErr := errors.New("unexpected restack error")

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			BranchGraph(gomock.Any(), gomock.Any()).
			Return(newBranchGraphBuilder("main").
				Branch("feature", "main").
				Build(t), nil)
		mockService.EXPECT().
			Restack(gomock.Any(), "feature").
			Return(nil, unexpectedErr)

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

		count, err := handler.Restack(t.Context(), &Request{
			Branch:          "feature",
			ContinueCommand: []string{"false"},
			Scope:           ScopeBranch,
		})

		require.Error(t, err)
		assert.Equal(t, 0, count)
		assert.ErrorContains(t, err, "restack branch \"feature\"")
		assert.ErrorIs(t, err, unexpectedErr)
	})

	t.Run("CheckoutError", func(t *testing.T) {
		log := silog.Nop()
		ctrl := gomock.NewController(t)

		checkoutErr := errors.New("checkout failed")

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			BranchGraph(gomock.Any(), gomock.Any()).
			Return(newBranchGraphBuilder("main").
				Branch("feature", "main").
				Build(t), nil)
		mockService.EXPECT().
			Restack(gomock.Any(), "feature").
			Return(&spice.RestackResponse{Base: "main"}, nil)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			RootDir().
			Return(t.TempDir())
		mockWorktree.EXPECT().
			Checkout(gomock.Any(), "feature").
			Return(checkoutErr)

		handler := &Handler{
			Log:      log,
			Worktree: mockWorktree,
			Store:    statetest.NewMemoryStore(t, "main", "", log),
			Service:  mockService,
		}

		count, err := handler.Restack(t.Context(), &Request{
			Branch:          "feature",
			ContinueCommand: []string{"false"},
			Scope:           ScopeBranch,
		})

		require.Error(t, err)
		assert.Equal(t, 0, count)
		assert.ErrorContains(t, err, "checkout branch feature")
		assert.ErrorIs(t, err, checkoutErr)
	})
}

type branchGraphBuilder struct {
	trunk     string
	items     []spice.BranchGraphItem
	worktrees map[string]string
}

func newBranchGraphBuilder(trunk string) *branchGraphBuilder {
	return &branchGraphBuilder{
		trunk:     trunk,
		worktrees: make(map[string]string),
	}
}

func (b *branchGraphBuilder) Branch(name, base string) *branchGraphBuilder {
	b.items = append(b.items, spice.BranchGraphItem{
		Name: name,
		Base: base,
	})
	return b
}

func (b *branchGraphBuilder) Worktree(branch, wt string) *branchGraphBuilder {
	b.worktrees[branch] = wt
	return b
}

func (b *branchGraphBuilder) Build(t testing.TB) *spice.BranchGraph {
	graph, err := spice.NewBranchGraph(t.Context(), &branchLoaderStub{
		trunk:     b.trunk,
		items:     b.items,
		worktrees: b.worktrees,
	}, &spice.BranchGraphOptions{IncludeWorktrees: true})
	require.NoError(t, err)
	return graph
}

type branchLoaderStub struct {
	trunk     string
	items     []spice.LoadBranchItem
	worktrees map[string]string
}

var _ spice.BranchLoader = (*branchLoaderStub)(nil)

func (b *branchLoaderStub) Trunk() string {
	return b.trunk
}

func (b *branchLoaderStub) LoadBranches(context.Context) ([]spice.LoadBranchItem, error) {
	return slices.Clone(b.items), nil
}

func (b *branchLoaderStub) LookupWorktrees(_ context.Context, branches []string) (map[string]string, error) {
	wts := make(map[string]string, len(branches))
	for _, branch := range branches {
		wt := b.worktrees[branch]
		if wt != "" {
			wts[branch] = wt
		}
	}
	return wts, nil
}
