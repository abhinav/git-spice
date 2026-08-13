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
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/spicetest"
	"go.abhg.dev/gs/internal/ui"
)

func TestMergedChangeHeadCheck(t *testing.T) {
	// ancestorCheck describes one expected ancestry query.
	type ancestorCheck struct {
		ancestor   git.Hash
		descendant git.Hash
		isAncestor bool
	}

	tests := []struct {
		name   string
		local  git.Hash
		remote git.Hash
		// Ancestry queries expected for non-equal local and remote heads.
		ancestorChecks []ancestorCheck
		want           mergedChangeHeadStatus
	}{
		{
			name:   "Exact",
			local:  git.Hash("aaa"),
			remote: git.Hash("aaa"),
			want:   mergedChangeHeadExact,
		},
		{
			name:   "RemoteContainsLocal",
			local:  git.Hash("aaa"),
			remote: git.Hash("bbb"),
			ancestorChecks: []ancestorCheck{
				{
					ancestor:   git.Hash("aaa"),
					descendant: git.Hash("bbb"),
					isAncestor: true,
				},
			},
			want: mergedChangeHeadRemoteContainsLocal,
		},
		{
			name:   "LocalContainsRemote",
			local:  git.Hash("bbb"),
			remote: git.Hash("aaa"),
			want:   mergedChangeHeadMismatch,
		},
		{
			name:   "Unknown",
			local:  git.Hash("aaa"),
			remote: git.Hash("bbb"),
			want:   mergedChangeHeadMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			mockRepo := NewMockGitRepository(ctrl)
			for _, check := range tt.ancestorChecks {
				mockRepo.EXPECT().
					IsAncestor(gomock.Any(), check.ancestor, check.descendant).
					Return(check.isAncestor)
			}
			if tt.local != tt.remote && len(tt.ancestorChecks) == 0 {
				mockRepo.EXPECT().
					IsAncestor(gomock.Any(), tt.local, tt.remote).
					Return(false)
			}

			assert.Equal(t, tt.want,
				mergedChangeHeadCheck(t.Context(), mockRepo, tt.local, tt.remote))
		})
	}
}

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
			BranchGraph(gomock.Any(), (*spice.BranchGraphOptions)(nil)).
			Return(spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
				Trunk: "main",
			}), nil)

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
			BranchGraph(gomock.Any(), (*spice.BranchGraphOptions)(nil)).
			Return(spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
				Trunk: "main",
				Branches: []spice.LoadBranchItem{{
					Name: "feature",
					Head: git.Hash("trunk"),
					Base: "main",
				}},
			}), nil)

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
		mockWorktree.EXPECT().
			CurrentBranch(gomock.Any()).
			Return("feature", nil)
		mockWorktree.EXPECT().
			RootDir().
			Return("/repo").
			AnyTimes()
		mockWorktree.EXPECT().
			CheckoutBranch(gomock.Any(), "main").
			Return(nil)

		mockStore := NewMockStore(ctrl)
		mockStore.EXPECT().
			Trunk().
			Return("main")

		mockRepo := newFetchOnlyRepoMocks(ctrl)
		mockRepo.EXPECT().
			LocalBranches(gomock.Any(), &git.LocalBranchesOptions{
				Patterns: []string{"feature"},
			}).
			Return(branchIter(git.LocalBranch{Name: "feature"}))

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			BranchGraph(gomock.Any(), (*spice.BranchGraphOptions)(nil)).
			Return(spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
				Trunk: "main",
				Branches: []spice.LoadBranchItem{
					{
						Name: "feature",
						Head: git.Hash("trunk"),
						Base: "main",
					},
					{
						Name: "child",
						Head: git.Hash("child-head"),
						Base: "feature",
					},
				},
			}), nil)
		mockRepo.EXPECT().
			IsAncestor(gomock.Any(), git.Hash("child-head"), git.Hash("trunk")).
			Return(false)

		mockDelete := NewMockDeleteHandler(ctrl)
		mockDelete.EXPECT().
			DeleteBranches(gomock.Any(), &branchdel.Request{
				Branches: []string{"feature"},
				Force:    true,
			}).
			Return(nil)

		mockRestack := NewMockRestackHandler(ctrl)
		mockRestack.EXPECT().
			RestackUpstack(gomock.Any(), &restack.UpstackRequest{
				Branch: "child",
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
			Restack:    mockRestack,
			Autostash:  mockAutostash,
			Remote:     "origin",
		}

		require.NoError(t, handler.SyncTrunk(t.Context(), &TrunkOptions{Restack: spice.RestackUpstack}))
		assert.Equal(t, 1, cleanupCalls)
		assert.Equal(t, "main", rescueBranch)
	})
}

func TestHandler_SyncTrunk_restackDeletedUpstacks(t *testing.T) {
	t.Run("Aboves", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockRepo := newFetchOnlyRepoMocks(ctrl)
		mockRepo.EXPECT().
			LocalBranches(gomock.Any(), &git.LocalBranchesOptions{
				Patterns: []string{"feature"},
			}).
			Return(branchIter(git.LocalBranch{Name: "feature"}))

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			CurrentBranch(gomock.Any()).
			Return("feature", nil)
		mockWorktree.EXPECT().
			RootDir().
			Return("/repo").
			AnyTimes()
		mockWorktree.EXPECT().
			CheckoutBranch(gomock.Any(), "main").
			Return(nil)

		mockStore := NewMockStore(ctrl)
		mockStore.EXPECT().
			Trunk().
			Return("main")

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			BranchGraph(gomock.Any(), (*spice.BranchGraphOptions)(nil)).
			Return(spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
				Trunk: "main",
				Branches: []spice.LoadBranchItem{
					{
						Name: "feature",
						Head: git.Hash("trunk"),
						Base: "main",
					},
					{
						Name: "child",
						Head: git.Hash("child-head"),
						Base: "feature",
					},
				},
			}), nil)
		mockRepo.EXPECT().
			IsAncestor(gomock.Any(), git.Hash("child-head"), git.Hash("trunk")).
			Return(false)

		mockDelete := NewMockDeleteHandler(ctrl)
		mockDelete.EXPECT().
			DeleteBranches(gomock.Any(), &branchdel.Request{
				Branches: []string{"feature"},
				Force:    true,
			}).
			Return(nil)

		mockRestack := NewMockRestackHandler(ctrl)
		mockRestack.EXPECT().
			RestackBranch(gomock.Any(), &restack.BranchRequest{
				Branch: "child",
			}).
			Return(nil)

		mockAutostash := NewMockAutostashHandler(ctrl)
		mockAutostash.EXPECT().
			BeginAutostash(gomock.Any(), &autostash.Options{
				Message:   "git-spice: autostash before sync",
				ResetMode: autostash.ResetHard,
			}).
			Return(func(*error, *autostash.CleanupOptions) {}, nil).
			AnyTimes()

		handler := &Handler{
			Log:        silogtest.New(t),
			View:       ui.NewFileView(t.Output()),
			Repository: mockRepo,
			Worktree:   mockWorktree,
			Store:      mockStore,
			Service:    mockService,
			Delete:     mockDelete,
			Restack:    mockRestack,
			Autostash:  mockAutostash,
			Remote:     "origin",
		}

		require.NoError(t, handler.SyncTrunk(t.Context(), &TrunkOptions{
			Restack: spice.RestackAboves,
		}))
	})

	t.Run("Upstack", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockRepo := newFetchOnlyRepoMocks(ctrl)
		mockRepo.EXPECT().
			LocalBranches(gomock.Any(), &git.LocalBranchesOptions{
				Patterns: []string{"feature"},
			}).
			Return(branchIter(git.LocalBranch{Name: "feature"}))

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			CurrentBranch(gomock.Any()).
			Return("feature", nil)
		mockWorktree.EXPECT().
			RootDir().
			Return("/repo").
			AnyTimes()
		mockWorktree.EXPECT().
			CheckoutBranch(gomock.Any(), "main").
			Return(nil)

		mockStore := NewMockStore(ctrl)
		mockStore.EXPECT().
			Trunk().
			Return("main")

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			BranchGraph(gomock.Any(), (*spice.BranchGraphOptions)(nil)).
			Return(spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
				Trunk: "main",
				Branches: []spice.LoadBranchItem{
					{
						Name: "feature",
						Head: git.Hash("trunk"),
						Base: "main",
					},
					{
						Name: "child",
						Head: git.Hash("child-head"),
						Base: "feature",
					},
				},
			}), nil)
		mockRepo.EXPECT().
			IsAncestor(gomock.Any(), git.Hash("child-head"), git.Hash("trunk")).
			Return(false)

		mockDelete := NewMockDeleteHandler(ctrl)
		mockDelete.EXPECT().
			DeleteBranches(gomock.Any(), &branchdel.Request{
				Branches: []string{"feature"},
				Force:    true,
			}).
			Return(nil)

		mockRestack := NewMockRestackHandler(ctrl)
		mockRestack.EXPECT().
			RestackUpstack(gomock.Any(), &restack.UpstackRequest{
				Branch: "child",
			}).
			Return(nil)

		mockAutostash := NewMockAutostashHandler(ctrl)
		mockAutostash.EXPECT().
			BeginAutostash(gomock.Any(), &autostash.Options{
				Message:   "git-spice: autostash before sync",
				ResetMode: autostash.ResetHard,
			}).
			Return(func(*error, *autostash.CleanupOptions) {}, nil).
			AnyTimes()

		handler := &Handler{
			Log:        silogtest.New(t),
			View:       ui.NewFileView(t.Output()),
			Repository: mockRepo,
			Worktree:   mockWorktree,
			Store:      mockStore,
			Service:    mockService,
			Delete:     mockDelete,
			Restack:    mockRestack,
			Autostash:  mockAutostash,
			Remote:     "origin",
		}

		require.NoError(t, handler.SyncTrunk(t.Context(), &TrunkOptions{
			Restack: spice.RestackUpstack,
		}))
	})

	t.Run("MultipleAdjacentDeletions", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockRepo := newFetchOnlyRepoMocks(ctrl)
		mockRepo.EXPECT().
			LocalBranches(gomock.Any(), &git.LocalBranchesOptions{
				Patterns: []string{"a", "b"},
			}).
			Return(branchIter(
				git.LocalBranch{Name: "a"},
				git.LocalBranch{Name: "b"},
			))

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			CurrentBranch(gomock.Any()).
			Return("c", nil)
		mockWorktree.EXPECT().
			RootDir().
			Return("/repo").
			AnyTimes()

		mockStore := NewMockStore(ctrl)
		mockStore.EXPECT().
			Trunk().
			Return("main")

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			BranchGraph(gomock.Any(), (*spice.BranchGraphOptions)(nil)).
			Return(spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
				Trunk: "main",
				Branches: []spice.LoadBranchItem{
					{
						Name: "a",
						Head: git.Hash("trunk"),
						Base: "main",
					},
					{
						Name: "b",
						Head: git.Hash("trunk"),
						Base: "a",
					},
					{
						Name: "c",
						Head: git.Hash("c-head"),
						Base: "b",
					},
				},
			}), nil)
		mockRepo.EXPECT().
			IsAncestor(gomock.Any(), git.Hash("c-head"), git.Hash("trunk")).
			Return(false)

		mockDelete := NewMockDeleteHandler(ctrl)
		mockDelete.EXPECT().
			DeleteBranches(gomock.Any(), &branchdel.Request{
				Branches: []string{"a", "b"},
				Force:    true,
			}).
			Return(nil)

		mockRestack := NewMockRestackHandler(ctrl)
		mockRestack.EXPECT().
			RestackUpstack(gomock.Any(), &restack.UpstackRequest{
				Branch: "c",
			}).
			Return(nil)

		mockAutostash := NewMockAutostashHandler(ctrl)
		mockAutostash.EXPECT().
			BeginAutostash(gomock.Any(), &autostash.Options{
				Message:   "git-spice: autostash before sync",
				ResetMode: autostash.ResetHard,
			}).
			Return(func(*error, *autostash.CleanupOptions) {}, nil).
			AnyTimes()

		handler := &Handler{
			Log:        silogtest.New(t),
			View:       ui.NewFileView(t.Output()),
			Repository: mockRepo,
			Worktree:   mockWorktree,
			Store:      mockStore,
			Service:    mockService,
			Delete:     mockDelete,
			Restack:    mockRestack,
			Autostash:  mockAutostash,
			Remote:     "origin",
		}

		require.NoError(t, handler.SyncTrunk(t.Context(), &TrunkOptions{
			Restack: spice.RestackUpstack,
		}))
	})

	t.Run("SkippedDeletion", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockRepo := newFetchOnlyRepoMocks(ctrl)
		mockRepo.EXPECT().
			LocalBranches(gomock.Any(), &git.LocalBranchesOptions{
				Patterns: []string{"feature"},
			}).
			Return(branchIter(git.LocalBranch{
				Name:     "feature",
				Worktree: "/other",
			}))

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			CurrentBranch(gomock.Any()).
			Return("child", nil)
		mockWorktree.EXPECT().
			RootDir().
			Return("/repo").
			AnyTimes()

		mockStore := NewMockStore(ctrl)
		mockStore.EXPECT().
			Trunk().
			Return("main")

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			BranchGraph(gomock.Any(), (*spice.BranchGraphOptions)(nil)).
			Return(spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
				Trunk: "main",
				Branches: []spice.LoadBranchItem{
					{
						Name: "feature",
						Head: git.Hash("trunk"),
						Base: "main",
					},
					{
						Name: "child",
						Head: git.Hash("child-head"),
						Base: "feature",
					},
				},
			}), nil)
		mockRepo.EXPECT().
			IsAncestor(gomock.Any(), git.Hash("child-head"), git.Hash("trunk")).
			Return(false)

		mockDelete := NewMockDeleteHandler(ctrl)
		mockDelete.EXPECT().
			DeleteBranches(gomock.Any(), &branchdel.Request{
				Branches: []string{},
				Force:    true,
			}).
			Return(nil)

		mockAutostash := NewMockAutostashHandler(ctrl)
		mockAutostash.EXPECT().
			BeginAutostash(gomock.Any(), &autostash.Options{
				Message:   "git-spice: autostash before sync",
				ResetMode: autostash.ResetHard,
			}).
			Return(func(*error, *autostash.CleanupOptions) {}, nil).
			AnyTimes()

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

		require.NoError(t, handler.SyncTrunk(t.Context(), &TrunkOptions{
			Restack: spice.RestackUpstack,
		}))
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
		IsAncestor(gomock.Any(), git.Hash("trunk"), git.Hash("trunk")).
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
	mockRepo.EXPECT().
		RemoteURL(gomock.Any(), "origin").
		Return("https://github.com/owner/repo", nil).
		AnyTimes()
	return mockRepo
}

func singleBranchIter(branch git.LocalBranch) iter.Seq2[git.LocalBranch, error] {
	return branchIter(branch)
}

func branchIter(branches ...git.LocalBranch) iter.Seq2[git.LocalBranch, error] {
	return func(yield func(git.LocalBranch, error) bool) {
		for _, branch := range branches {
			if !yield(branch, nil) {
				return
			}
		}
	}
}
