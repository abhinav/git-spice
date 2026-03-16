package sync

import (
	"context"
	"iter"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/autostash"
	branchdel "go.abhg.dev/gs/internal/handler/delete"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/ui"
)

func TestHandler_SyncTrunk_autostashLazy(t *testing.T) {
	t.Run("FetchOnlyDoesNotStart", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockAutostash := NewMockAutostashHandler(ctrl)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			CurrentBranch(gomock.Any()).
			Return("feature", nil)

		mockStore := NewMockStore(ctrl)
		mockStore.EXPECT().
			Trunk().
			Return("main")

		mockRepo := newFetchOnlyRepoMocks(ctrl)
		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			LoadBranches(gomock.Any()).
			Return(nil, nil)

		handler := &Handler{
			Log:        silogtest.New(t),
			View:       ui.NewFileView(t.Output()),
			Repository: mockRepo,
			Worktree:   mockWorktree,
			Store:      mockStore,
			Service:    mockService,
			Delete:     NewMockDeleteHandler(ctrl),
			Restack:    NewMockRestackHandler(ctrl),
			Autostash:  mockAutostash,
			Remote:     "origin",
		}

		require.NoError(t, handler.SyncTrunk(t.Context(), nil))
	})

	t.Run("DeleteStartsOnce", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockAutostash := NewMockAutostashHandler(ctrl)
		var cleanupCalls int
		var rescueBranch string
		mockAutostash.EXPECT().
			BeginAutostash(gomock.Any(), &autostash.Options{
				Message:   "git-spice: autostash before sync",
				ResetMode: autostash.ResetHard,
			}).
			DoAndReturn(func(context.Context, *autostash.Options) (func(*error, *autostash.CleanupOptions), error) {
				return func(_ *error, opts *autostash.CleanupOptions) {
					cleanupCalls++
					if opts != nil {
						rescueBranch = opts.RescueBranch
					}
				}, nil
			}).
			Times(1)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			CurrentBranch(gomock.Any()).
			Return("feature", nil)
		mockWorktree.EXPECT().
			RootDir().
			Return("/repo").
			AnyTimes()

		mockStore := NewMockStore(ctrl)
		mockStore.EXPECT().
			Trunk().
			Return("main")

		mockRepo := newFetchOnlyRepoMocks(ctrl)
		mockRepo.EXPECT().
			LocalBranches(gomock.Any(), &git.LocalBranchesOptions{
				Patterns: []string{"feature"},
			}).
			Return(singleBranchIter(git.LocalBranch{Name: "feature"}))

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			LoadBranches(gomock.Any()).
			Return([]spice.LoadBranchItem{{
				Name: "feature",
				Head: git.Hash("trunk"),
				Base: "main",
			}}, nil)

		mockDelete := NewMockDeleteHandler(ctrl)
		mockDelete.EXPECT().
			DeleteBranches(gomock.Any(), &branchdel.Request{
				Branches: []string{"feature"},
				Force:    true,
			}).
			Return(nil)

		handler := &Handler{
			Log:        silogtest.New(t),
			View:       ui.NewFileView(t.Output()),
			Repository: mockRepo,
			Worktree:   mockWorktree,
			Store:      mockStore,
			Service:    mockService,
			Delete:     mockDelete,
			Restack:    NewMockRestackHandler(ctrl),
			Autostash:  mockAutostash,
			Remote:     "origin",
		}

		require.NoError(t, handler.SyncTrunk(t.Context(), nil))
		assert.Equal(t, 1, cleanupCalls)
		assert.Equal(t, "main", rescueBranch)
	})

	t.Run("RestackStartsOnce", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockAutostash := NewMockAutostashHandler(ctrl)
		var cleanupCalls int
		var rescueBranch string
		mockAutostash.EXPECT().
			BeginAutostash(gomock.Any(), &autostash.Options{
				Message:   "git-spice: autostash before sync",
				ResetMode: autostash.ResetHard,
			}).
			DoAndReturn(func(context.Context, *autostash.Options) (func(*error, *autostash.CleanupOptions), error) {
				return func(_ *error, opts *autostash.CleanupOptions) {
					cleanupCalls++
					if opts != nil {
						rescueBranch = opts.RescueBranch
					}
				}, nil
			}).
			Times(1)

		mockWorktree := NewMockGitWorktree(ctrl)
		gomock.InOrder(
			mockWorktree.EXPECT().
				CurrentBranch(gomock.Any()).
				Return("feature", nil),
			mockWorktree.EXPECT().
				CurrentBranch(gomock.Any()).
				Return("feature", nil),
		)

		mockStore := NewMockStore(ctrl)
		mockStore.EXPECT().
			Trunk().
			Return("main")

		mockRepo := newFetchOnlyRepoMocks(ctrl)
		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			LoadBranches(gomock.Any()).
			Return(nil, nil)

		mockRestack := NewMockRestackHandler(ctrl)
		mockRestack.EXPECT().
			RestackStack(gomock.Any(), "feature").
			Return(nil)

		handler := &Handler{
			Log:        silogtest.New(t),
			View:       ui.NewFileView(t.Output()),
			Repository: mockRepo,
			Worktree:   mockWorktree,
			Store:      mockStore,
			Service:    mockService,
			Delete:     NewMockDeleteHandler(ctrl),
			Restack:    mockRestack,
			Autostash:  mockAutostash,
			Remote:     "origin",
		}

		require.NoError(t, handler.SyncTrunk(t.Context(), &TrunkOptions{Restack: true}))
		assert.Equal(t, 1, cleanupCalls)
		assert.Equal(t, "feature", rescueBranch)
	})
}

func newFetchOnlyRepoMocks(ctrl *gomock.Controller) *MockGitRepository {
	mockRepo := NewMockGitRepository(ctrl)
	mockRepo.EXPECT().
		PeelToCommit(gomock.Any(), "main").
		Return(git.Hash("trunk"), nil).
		AnyTimes()
	mockRepo.EXPECT().
		PeelToCommit(gomock.Any(), "origin/main").
		Return(git.Hash("trunk"), nil).
		AnyTimes()
	mockRepo.EXPECT().
		LocalBranches(gomock.Any(), (*git.LocalBranchesOptions)(nil)).
		Return(singleBranchIter(git.LocalBranch{Name: "main"})).
		AnyTimes()
	mockRepo.EXPECT().
		IsAncestor(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(true).
		AnyTimes()
	mockRepo.EXPECT().
		Fetch(gomock.Any(), git.FetchOptions{
			Remote: "origin",
			Refspecs: []git.Refspec{
				git.Refspec("main:main"),
			},
		}).
		Return(nil).
		AnyTimes()
	return mockRepo
}

func singleBranchIter(branch git.LocalBranch) iter.Seq2[git.LocalBranch, error] {
	return func(yield func(git.LocalBranch, error) bool) {
		yield(branch, nil)
	}
}
