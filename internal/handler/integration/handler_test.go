package integration

import (
	"errors"
	"iter"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	gomock "go.uber.org/mock/gomock"
)

// newHandler returns a handler whose worktree-safety guard is satisfied by
// a single, borrowable worktree (the common case). Tests that need a
// different worktree topology use newHandlerRaw and wire Worktrees/RootDir
// themselves.
func newHandler(t *testing.T) (*Handler, *handlerMocks) {
	t.Helper()
	h, mocks := newHandlerRaw(t)
	expectBorrowableWorktree(mocks)
	return h, mocks
}

func newHandlerRaw(t *testing.T) (*Handler, *handlerMocks) {
	t.Helper()
	mockCtrl := gomock.NewController(t)
	mocks := &handlerMocks{
		Repository: NewMockGitRepository(mockCtrl),
		Worktree:   NewMockGitWorktree(mockCtrl),
		Store:      NewMockStore(mockCtrl),
		Service:    NewMockService(mockCtrl),
	}
	h := &Handler{
		Log:        silogtest.New(t),
		Repository: mocks.Repository,
		Worktree:   mocks.Worktree,
		Store:      mocks.Store,
		Service:    mocks.Service,
	}
	return h, mocks
}

type handlerMocks struct {
	Repository *MockGitRepository
	Worktree   *MockGitWorktree
	Store      *MockStore
	Service    *MockService
}

func TestHandler_Create_rejectsTrunkName(t *testing.T) {
	h, mocks := newHandler(t)
	mocks.Store.EXPECT().Trunk().Return("main").AnyTimes()

	err := h.Create(t.Context(), &CreateRequest{Name: "main"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not equal trunk")
}

func TestHandler_Create_rejectsEmptyName(t *testing.T) {
	h, _ := newHandler(t)

	err := h.Create(t.Context(), &CreateRequest{Name: ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestHandler_Create_rejectsAlreadyConfigured(t *testing.T) {
	h, mocks := newHandler(t)
	mocks.Store.EXPECT().Trunk().Return("main").AnyTimes()
	mocks.Store.EXPECT().
		Integration(gomock.Any()).
		Return(&state.IntegrationInfo{Name: "preview"}, nil)

	err := h.Create(t.Context(), &CreateRequest{Name: "preview"})
	require.ErrorIs(t, err, ErrAlreadyConfigured)
}

func TestHandler_Create_validatesTips(t *testing.T) {
	h, mocks := newHandler(t)
	mocks.Store.EXPECT().Trunk().Return("main").AnyTimes()
	mocks.Store.EXPECT().
		Integration(gomock.Any()).
		Return(nil, state.ErrNotExist)
	mocks.Service.EXPECT().
		LookupBranch(gomock.Any(), "nonexistent").
		Return(nil, state.ErrNotExist)

	err := h.Create(t.Context(), &CreateRequest{
		Name: "preview",
		Tips: []string{"nonexistent"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not tracked")
}

func TestHandler_Create_persists(t *testing.T) {
	h, mocks := newHandler(t)
	mocks.Store.EXPECT().Trunk().Return("main").AnyTimes()
	mocks.Store.EXPECT().
		Integration(gomock.Any()).
		Return(nil, state.ErrNotExist)
	mocks.Service.EXPECT().
		LookupBranch(gomock.Any(), "feat-a").
		Return(&spice.LookupBranchResponse{}, nil)
	mocks.Service.EXPECT().
		LookupBranch(gomock.Any(), "feat-b").
		Return(&spice.LookupBranchResponse{}, nil)

	mocks.Store.EXPECT().
		SetIntegration(gomock.Any(), &state.IntegrationInfo{
			Name: "preview",
			Tips: []state.IntegrationTip{
				{Name: "feat-a"},
				{Name: "feat-b"},
			},
		}).
		Return(nil)

	require.NoError(t, h.Create(t.Context(), &CreateRequest{
		Name: "preview",
		Tips: []string{"feat-a", "feat-b"},
	}))
}

func TestHandler_Checkout(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{Name: "preview"}, nil)
		mocks.Repository.EXPECT().
			PeelToCommit(gomock.Any(), "preview").
			Return(git.Hash("abc"), nil)
		mocks.Worktree.EXPECT().
			CheckoutBranch(gomock.Any(), "preview").
			Return(nil)

		require.NoError(t, h.Checkout(t.Context()))
	})

	t.Run("NotConfigured", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(nil, state.ErrNotExist)

		err := h.Checkout(t.Context())
		require.ErrorIs(t, err, ErrNotConfigured)
	})

	t.Run("BranchMissing", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{Name: "preview"}, nil)
		mocks.Repository.EXPECT().
			PeelToCommit(gomock.Any(), "preview").
			Return(git.Hash(""), errors.New("not found"))

		err := h.Checkout(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not exist")
	})
}

func TestHandler_Delete_notConfigured(t *testing.T) {
	h, mocks := newHandler(t)
	mocks.Store.EXPECT().
		Integration(gomock.Any()).
		Return(nil, state.ErrNotExist)

	err := h.Delete(t.Context())
	require.ErrorIs(t, err, ErrNotConfigured)
}

func TestHandler_Delete_clears(t *testing.T) {
	h, mocks := newHandler(t)
	mocks.Store.EXPECT().
		Integration(gomock.Any()).
		Return(&state.IntegrationInfo{Name: "preview"}, nil)
	mocks.Store.EXPECT().
		SetIntegration(gomock.Any(), gomock.Nil()).
		Return(nil)

	require.NoError(t, h.Delete(t.Context()))
}

func TestHandler_AddTip(t *testing.T) {
	t.Run("AppendsToList", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().Trunk().Return("main").AnyTimes()
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{
				Name: "preview",
				Tips: []state.IntegrationTip{{Name: "feat-a"}},
			}, nil)
		mocks.Service.EXPECT().
			LookupBranch(gomock.Any(), "feat-b").
			Return(&spice.LookupBranchResponse{}, nil)
		mocks.Store.EXPECT().
			SetIntegration(gomock.Any(), &state.IntegrationInfo{
				Name: "preview",
				Tips: []state.IntegrationTip{
					{Name: "feat-a"},
					{Name: "feat-b"},
				},
			}).
			Return(nil)

		require.NoError(t, h.AddTip(t.Context(), "feat-b"))
	})

	t.Run("RejectsDuplicate", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{
				Name: "preview",
				Tips: []state.IntegrationTip{{Name: "feat-a"}},
			}, nil)

		err := h.AddTip(t.Context(), "feat-a")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already configured")
	})

	t.Run("RejectsTrunk", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().Trunk().Return("main").AnyTimes()
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{Name: "preview"}, nil)

		err := h.AddTip(t.Context(), "main")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must not equal trunk")
	})

	t.Run("RejectsIntegrationName", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().Trunk().Return("main").AnyTimes()
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{Name: "preview"}, nil)

		err := h.AddTip(t.Context(), "preview")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must not equal integration")
	})
}

func TestHandler_RemoveTip(t *testing.T) {
	t.Run("Removes", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{
				Name: "preview",
				Tips: []state.IntegrationTip{
					{Name: "feat-a"},
					{Name: "feat-b"},
				},
			}, nil)
		mocks.Store.EXPECT().
			SetIntegration(gomock.Any(), &state.IntegrationInfo{
				Name: "preview",
				Tips: []state.IntegrationTip{{Name: "feat-b"}},
			}).
			Return(nil)

		require.NoError(t, h.RemoveTip(t.Context(), "feat-a"))
	})

	t.Run("NotConfigured", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(nil, state.ErrNotExist)

		err := h.RemoveTip(t.Context(), "feat-a")
		require.ErrorIs(t, err, ErrNotConfigured)
	})

	t.Run("UnknownTip", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{
				Name: "preview",
				Tips: []state.IntegrationTip{{Name: "feat-a"}},
			}, nil)

		err := h.RemoveTip(t.Context(), "feat-b")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "is not configured")
	})
}

func TestHandler_Show(t *testing.T) {
	t.Run("ReportsDrift", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{
				Name: "preview",
				Tips: []state.IntegrationTip{
					{Name: "feat-a", Hash: "stored-a"},
					{Name: "feat-b", Hash: "stored-b"},
				},
			}, nil)
		mocks.Repository.EXPECT().
			PeelToCommit(gomock.Any(), "feat-a").
			Return(git.Hash("stored-a"), nil)
		mocks.Repository.EXPECT().
			PeelToCommit(gomock.Any(), "feat-b").
			Return(git.Hash("current-b"), nil)

		st, err := h.Show(t.Context())
		require.NoError(t, err)

		require.Len(t, st.Tips, 2)
		assert.False(t, st.Tips[0].Drifted())
		assert.True(t, st.Tips[1].Drifted())
	})

	t.Run("MissingTip", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{
				Name: "preview",
				Tips: []state.IntegrationTip{{Name: "gone", Hash: "stored"}},
			}, nil)
		mocks.Repository.EXPECT().
			PeelToCommit(gomock.Any(), "gone").
			Return(git.Hash(""), errors.New("not found"))

		st, err := h.Show(t.Context())
		require.NoError(t, err)
		require.Len(t, st.Tips, 1)
		assert.True(t, st.Tips[0].Missing)
		assert.True(t, st.Tips[0].Drifted())
	})
}

func TestHandler_Rebuild(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{
				Name: "preview",
				Tips: []state.IntegrationTip{
					{Name: "feat-a"},
					{Name: "feat-b"},
				},
			}, nil)
		mocks.Store.EXPECT().
			PendingIntegrationRebuild(gomock.Any()).
			Return(nil, state.ErrNotExist)
		mocks.Worktree.EXPECT().
			CurrentBranch(gomock.Any()).
			Return("main", nil)
		mocks.Worktree.EXPECT().
			IsClean(gomock.Any()).
			Return(true, nil)
		mocks.Store.EXPECT().Trunk().Return("main").AnyTimes()
		mocks.Repository.EXPECT().
			PeelToCommit(gomock.Any(), "main").
			Return(git.Hash("trunk-hash"), nil)
		mocks.Service.EXPECT().
			LookupBranch(gomock.Any(), "feat-a").
			Return(&spice.LookupBranchResponse{}, nil)
		mocks.Repository.EXPECT().
			PeelToCommit(gomock.Any(), "feat-a").
			Return(git.Hash("hash-a"), nil)
		mocks.Service.EXPECT().
			LookupBranch(gomock.Any(), "feat-b").
			Return(&spice.LookupBranchResponse{}, nil)
		mocks.Repository.EXPECT().
			PeelToCommit(gomock.Any(), "feat-b").
			Return(git.Hash("hash-b"), nil)

		mocks.Worktree.EXPECT().
			CheckoutNewBranch(gomock.Any(), git.CheckoutNewBranchRequest{
				Name:       "preview",
				StartPoint: "trunk-hash",
				Force:      true,
			}).
			Return(nil)
		mocks.Worktree.EXPECT().
			Merge(gomock.Any(), gomock.Cond(func(o git.MergeOptions) bool {
				return len(o.Refs) == 1 && o.Refs[0] == "hash-a" &&
					o.NoFF && o.EnableRerere
			})).
			Return(nil)
		mocks.Worktree.EXPECT().
			Merge(gomock.Any(), gomock.Cond(func(o git.MergeOptions) bool {
				return len(o.Refs) == 1 && o.Refs[0] == "hash-b" &&
					o.NoFF && o.EnableRerere
			})).
			Return(nil)

		mocks.Store.EXPECT().
			SetIntegration(gomock.Any(), gomock.Cond(func(info *state.IntegrationInfo) bool {
				return assert.Equal(t, []state.IntegrationTip{
					{Name: "feat-a", Hash: "hash-a"},
					{Name: "feat-b", Hash: "hash-b"},
				}, info.Tips)
			})).
			Return(nil)
		mocks.Store.EXPECT().
			ClearPendingIntegrationRebuild(gomock.Any()).
			Return(nil)

		mocks.Worktree.EXPECT().
			CheckoutBranch(gomock.Any(), "main").
			Return(nil)

		res, err := h.Rebuild(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "preview", res.Name)
		assert.Equal(t, []git.Hash{"hash-a", "hash-b"}, res.TipHashes)
	})

	t.Run("AlreadyOnIntegrationBranch", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{
				Name: "preview",
				Tips: []state.IntegrationTip{{Name: "feat-a"}},
			}, nil)
		mocks.Store.EXPECT().
			PendingIntegrationRebuild(gomock.Any()).
			Return(nil, state.ErrNotExist)
		mocks.Worktree.EXPECT().
			CurrentBranch(gomock.Any()).
			Return("preview", nil)
		mocks.Worktree.EXPECT().
			IsClean(gomock.Any()).
			Return(true, nil)
		mocks.Store.EXPECT().Trunk().Return("main").AnyTimes()
		mocks.Repository.EXPECT().
			PeelToCommit(gomock.Any(), "main").
			Return(git.Hash("trunk-hash"), nil)
		mocks.Service.EXPECT().
			LookupBranch(gomock.Any(), "feat-a").
			Return(&spice.LookupBranchResponse{}, nil)
		mocks.Repository.EXPECT().
			PeelToCommit(gomock.Any(), "feat-a").
			Return(git.Hash("hash-a"), nil)

		mocks.Worktree.EXPECT().
			CheckoutNewBranch(gomock.Any(), git.CheckoutNewBranchRequest{
				Name:       "preview",
				StartPoint: "trunk-hash",
				Force:      true,
			}).
			Return(nil)
		mocks.Worktree.EXPECT().
			Merge(gomock.Any(), gomock.Any()).
			Return(nil)
		mocks.Store.EXPECT().
			SetIntegration(gomock.Any(), gomock.Any()).
			Return(nil)
		mocks.Store.EXPECT().
			ClearPendingIntegrationRebuild(gomock.Any()).
			Return(nil)
		// No final CheckoutBranch call expected since we started on
		// the integration branch already.

		_, err := h.Rebuild(t.Context())
		require.NoError(t, err)
	})

	t.Run("RefusesDirtyWorktree", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{Name: "preview"}, nil)
		mocks.Store.EXPECT().
			PendingIntegrationRebuild(gomock.Any()).
			Return(nil, state.ErrNotExist)
		mocks.Worktree.EXPECT().
			CurrentBranch(gomock.Any()).
			Return("main", nil)
		mocks.Worktree.EXPECT().
			IsClean(gomock.Any()).
			Return(false, nil)

		_, err := h.Rebuild(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "uncommitted")
	})

	t.Run("ConflictSavesPending", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{
				Name: "preview",
				Tips: []state.IntegrationTip{
					{Name: "feat-a"},
					{Name: "feat-b"},
				},
			}, nil)
		mocks.Store.EXPECT().
			PendingIntegrationRebuild(gomock.Any()).
			Return(nil, state.ErrNotExist)
		mocks.Worktree.EXPECT().
			CurrentBranch(gomock.Any()).
			Return("main", nil)
		mocks.Worktree.EXPECT().
			IsClean(gomock.Any()).
			Return(true, nil)
		mocks.Store.EXPECT().Trunk().Return("main").AnyTimes()
		mocks.Repository.EXPECT().
			PeelToCommit(gomock.Any(), "main").
			Return(git.Hash("trunk"), nil)
		mocks.Service.EXPECT().
			LookupBranch(gomock.Any(), "feat-a").
			Return(&spice.LookupBranchResponse{}, nil)
		mocks.Repository.EXPECT().
			PeelToCommit(gomock.Any(), "feat-a").
			Return(git.Hash("a"), nil)
		mocks.Service.EXPECT().
			LookupBranch(gomock.Any(), "feat-b").
			Return(&spice.LookupBranchResponse{}, nil)
		mocks.Repository.EXPECT().
			PeelToCommit(gomock.Any(), "feat-b").
			Return(git.Hash("b"), nil)

		mocks.Worktree.EXPECT().
			CheckoutNewBranch(gomock.Any(), gomock.Any()).
			Return(nil)
		mocks.Worktree.EXPECT().
			Merge(gomock.Any(), gomock.Any()).
			Return(&git.MergeConflictError{
				Refs:          []string{"a"},
				ConflictPaths: []string{"shared.txt"},
			})

		// Pending state saved with the tip AFTER the conflicting one
		// recorded as next.
		mocks.Store.EXPECT().
			SetPendingIntegrationRebuild(gomock.Any(),
				gomock.Cond(func(rb *state.IntegrationRebuild) bool {
					return rb.Integration == "preview" &&
						rb.NextTipIndex == 1 &&
						len(rb.Tips) == 2
				})).
			Return(nil)
		// No CheckoutBranch: the conflict is left in the worktree.

		_, err := h.Rebuild(t.Context())
		require.Error(t, err)
		var conflict *ConflictError
		assert.True(t, errors.As(err, &conflict))
		assert.Equal(t, "feat-a", conflict.Tip)
	})

	t.Run("ResumeAfterConflict", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{
				Name: "preview",
				Tips: []state.IntegrationTip{
					{Name: "feat-a"},
					{Name: "feat-b"},
				},
			}, nil)
		mocks.Store.EXPECT().
			PendingIntegrationRebuild(gomock.Any()).
			Return(&state.IntegrationRebuild{
				Integration: "preview",
				Tips: []state.IntegrationTip{
					{Name: "feat-a", Hash: "a"},
					{Name: "feat-b", Hash: "b"},
				},
				NextTipIndex: 1,
			}, nil)
		mocks.Worktree.EXPECT().
			IsClean(gomock.Any()).
			Return(true, nil)
		mocks.Worktree.EXPECT().
			CurrentBranch(gomock.Any()).
			Return("preview", nil)

		// Resume picks up at feat-b only.
		mocks.Worktree.EXPECT().
			Merge(gomock.Any(), gomock.Cond(func(o git.MergeOptions) bool {
				return o.Refs[0] == "b"
			})).
			Return(nil)
		mocks.Store.EXPECT().
			SetIntegration(gomock.Any(), gomock.Cond(func(info *state.IntegrationInfo) bool {
				return assert.Equal(t, []state.IntegrationTip{
					{Name: "feat-a", Hash: "a"},
					{Name: "feat-b", Hash: "b"},
				}, info.Tips)
			})).
			Return(nil)
		mocks.Store.EXPECT().
			ClearPendingIntegrationRebuild(gomock.Any()).
			Return(nil)

		_, err := h.Rebuild(t.Context())
		require.NoError(t, err)
	})

	t.Run("RefusesWhenIntegrationBranchInAnotherWorktree", func(t *testing.T) {
		h, mocks := newHandlerRaw(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{Name: "preview"}, nil)
		mocks.Worktree.EXPECT().RootDir().Return("/repo").AnyTimes()
		mocks.Repository.EXPECT().
			Worktrees(gomock.Any()).
			Return(worktreesSeq(
				&git.WorktreeListItem{Path: "/repo", Branch: "main"},
				&git.WorktreeListItem{Path: "/wt/preview", Branch: "preview"},
			))

		_, err := h.Rebuild(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "another worktree")
	})

	t.Run("RefusesWhenHijackingFeatureWorktree", func(t *testing.T) {
		h, mocks := newHandlerRaw(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{Name: "preview"}, nil)
		mocks.Store.EXPECT().Trunk().Return("main").AnyTimes()
		mocks.Worktree.EXPECT().RootDir().Return("/wt/feat").AnyTimes()
		mocks.Repository.EXPECT().
			Worktrees(gomock.Any()).
			Return(worktreesSeq(
				&git.WorktreeListItem{Path: "/repo", Branch: "main"},
				&git.WorktreeListItem{Path: "/wt/feat", Branch: "feat-x"},
			))
		mocks.Service.EXPECT().
			LookupBranch(gomock.Any(), "feat-x").
			Return(&spice.LookupBranchResponse{}, nil)

		_, err := h.Rebuild(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "would be reverted")
	})
}

func TestHandler_Submit(t *testing.T) {
	t.Run("ForceWithLease", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{
				Name:           "preview",
				LastPushedHash: "old",
			}, nil)
		mocks.Store.EXPECT().
			Remote().
			Return(state.Remote{Upstream: "origin", Push: "origin"}, nil)
		mocks.Repository.EXPECT().
			PeelToCommit(gomock.Any(), "preview").
			Return(git.Hash("new"), nil)
		mocks.Worktree.EXPECT().
			Push(gomock.Any(), gomock.Cond(func(opts git.PushOptions) bool {
				return opts.Remote == "origin" &&
					opts.ForceWithLease == "preview:old" &&
					opts.Refspec == "preview:preview"
			})).
			Return(nil)
		mocks.Store.EXPECT().
			SetIntegration(gomock.Any(), gomock.Cond(func(info *state.IntegrationInfo) bool {
				return info.LastPushedHash == "new"
			})).
			Return(nil)

		require.NoError(t, h.Submit(t.Context()))
	})

	t.Run("FirstPushPlain", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{Name: "preview"}, nil)
		mocks.Store.EXPECT().
			Remote().
			Return(state.Remote{Upstream: "origin", Push: "origin"}, nil)
		mocks.Repository.EXPECT().
			PeelToCommit(gomock.Any(), "preview").
			Return(git.Hash("hash"), nil)
		// No LastPushedHash + no remote branch → plain push proceeds.
		mocks.Repository.EXPECT().
			ListRemoteRefs(gomock.Any(), "origin", gomock.Any()).
			Return(emptyRemoteRefs())
		mocks.Worktree.EXPECT().
			Push(gomock.Any(), gomock.Cond(func(opts git.PushOptions) bool {
				return opts.ForceWithLease == "" && !opts.Force
			})).
			Return(nil)
		mocks.Store.EXPECT().
			SetIntegration(gomock.Any(), gomock.Any()).
			Return(nil)

		require.NoError(t, h.Submit(t.Context()))
	})

	t.Run("ForkModeUsesPushRemote", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{Name: "preview"}, nil)
		mocks.Store.EXPECT().
			Remote().
			Return(state.Remote{Upstream: "upstream", Push: "origin"}, nil)
		mocks.Repository.EXPECT().
			PeelToCommit(gomock.Any(), "preview").
			Return(git.Hash("hash"), nil)
		mocks.Repository.EXPECT().
			ListRemoteRefs(gomock.Any(), "origin", gomock.Any()).
			Return(emptyRemoteRefs())
		mocks.Worktree.EXPECT().
			Push(gomock.Any(), gomock.Cond(func(opts git.PushOptions) bool {
				return opts.Remote == "origin"
			})).
			Return(nil)
		mocks.Store.EXPECT().
			SetIntegration(gomock.Any(), gomock.Any()).
			Return(nil)

		require.NoError(t, h.Submit(t.Context()))
	})

	t.Run("RejectedWhenRemoteAheadOfState", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{Name: "preview"}, nil)
		mocks.Store.EXPECT().
			Remote().
			Return(state.Remote{Upstream: "origin", Push: "origin"}, nil)
		mocks.Repository.EXPECT().
			PeelToCommit(gomock.Any(), "preview").
			Return(git.Hash("local-hash"), nil)
		// No LastPushedHash + remote branch exists → reject.
		mocks.Repository.EXPECT().
			ListRemoteRefs(gomock.Any(), "origin", gomock.Any()).
			Return(singleRemoteRef(git.RemoteRef{
				Name: "refs/heads/preview",
				Hash: git.Hash("remote-hash"),
			}))

		err := h.Submit(t.Context())
		require.Error(t, err)
		var rejected *PushRejectedError
		require.ErrorAs(t, err, &rejected)
		assert.Equal(t, "preview", rejected.UpstreamBranch)
		assert.Equal(t, "origin", rejected.Remote)
		assert.Equal(t, git.Hash("remote-hash"), rejected.RemoteHash)
		assert.Equal(t, git.Hash("local-hash"), rejected.LocalHash)
	})

	t.Run("ForceWithLeaseSkipsRemoteProbe", func(t *testing.T) {
		// With LastPushedHash set, Submit uses --force-with-lease;
		// no remote probe is needed.
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{
				Name:           "preview",
				LastPushedHash: "old",
			}, nil)
		mocks.Store.EXPECT().
			Remote().
			Return(state.Remote{Upstream: "origin", Push: "origin"}, nil)
		mocks.Repository.EXPECT().
			PeelToCommit(gomock.Any(), "preview").
			Return(git.Hash("new"), nil)
		mocks.Worktree.EXPECT().
			Push(gomock.Any(), gomock.Any()).
			Return(nil)
		mocks.Store.EXPECT().
			SetIntegration(gomock.Any(), gomock.Any()).
			Return(nil)

		require.NoError(t, h.Submit(t.Context()))
	})
}

func TestHandler_MarkPushed(t *testing.T) {
	t.Run("ExplicitHash", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{Name: "preview"}, nil)
		mocks.Store.EXPECT().
			SetIntegration(gomock.Any(), gomock.Cond(func(info *state.IntegrationInfo) bool {
				return info.LastPushedHash == "explicit-hash"
			})).
			Return(nil)

		require.NoError(t,
			h.MarkPushed(t.Context(), git.Hash("explicit-hash")))
	})

	t.Run("AutoDiscoversFromRemote", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{Name: "preview"}, nil)
		mocks.Store.EXPECT().
			Remote().
			Return(state.Remote{Upstream: "origin", Push: "origin"}, nil)
		mocks.Repository.EXPECT().
			ListRemoteRefs(gomock.Any(), "origin", gomock.Any()).
			Return(singleRemoteRef(git.RemoteRef{
				Name: "refs/heads/preview",
				Hash: git.Hash("discovered"),
			}))
		mocks.Store.EXPECT().
			SetIntegration(gomock.Any(), gomock.Cond(func(info *state.IntegrationInfo) bool {
				return info.LastPushedHash == "discovered"
			})).
			Return(nil)

		require.NoError(t, h.MarkPushed(t.Context(), ""))
	})

	t.Run("AutoDiscoverFailsWhenRemoteEmpty", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{Name: "preview"}, nil)
		mocks.Store.EXPECT().
			Remote().
			Return(state.Remote{Upstream: "origin", Push: "origin"}, nil)
		mocks.Repository.EXPECT().
			ListRemoteRefs(gomock.Any(), "origin", gomock.Any()).
			Return(emptyRemoteRefs())

		err := h.MarkPushed(t.Context(), "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no branch")
	})

	t.Run("UsesUpstreamBranchOverride", func(t *testing.T) {
		h, mocks := newHandler(t)
		mocks.Store.EXPECT().
			Integration(gomock.Any()).
			Return(&state.IntegrationInfo{
				Name:           "preview",
				UpstreamBranch: "remote-preview",
			}, nil)
		mocks.Store.EXPECT().
			Remote().
			Return(state.Remote{Upstream: "origin", Push: "origin"}, nil)
		mocks.Repository.EXPECT().
			ListRemoteRefs(gomock.Any(), "origin",
				gomock.Cond(func(opts *git.ListRemoteRefsOptions) bool {
					return len(opts.Patterns) == 1 && opts.Patterns[0] == "remote-preview"
				})).
			Return(singleRemoteRef(git.RemoteRef{
				Name: "refs/heads/remote-preview",
				Hash: git.Hash("hash"),
			}))
		mocks.Store.EXPECT().
			SetIntegration(gomock.Any(), gomock.Any()).
			Return(nil)

		require.NoError(t, h.MarkPushed(t.Context(), ""))
	})
}

func TestHandler_MaybeRebuild_noConfig(t *testing.T) {
	h, mocks := newHandler(t)
	mocks.Store.EXPECT().
		Integration(gomock.Any()).
		Return(nil, state.ErrNotExist)

	require.NoError(t, h.MaybeRebuild(t.Context()))
}

func TestHandler_MaybeRebuild_noDrift(t *testing.T) {
	h, mocks := newHandler(t)
	mocks.Store.EXPECT().
		Integration(gomock.Any()).
		Return(&state.IntegrationInfo{
			Name: "preview",
			Tips: []state.IntegrationTip{{Name: "feat-a", Hash: "abc"}},
		}, nil)
	mocks.Repository.EXPECT().
		PeelToCommit(gomock.Any(), "feat-a").
		Return(git.Hash("abc"), nil)

	require.NoError(t, h.MaybeRebuild(t.Context()))
}

func TestHandler_MaybeRebuildAndSubmit_skipsWhenNotPreviouslyPushed(t *testing.T) {
	h, mocks := newHandler(t)
	// First call: MaybeRebuild - returns no-drift no-op
	mocks.Store.EXPECT().
		Integration(gomock.Any()).
		Return(&state.IntegrationInfo{
			Name: "preview",
			Tips: []state.IntegrationTip{{Name: "feat-a", Hash: "abc"}},
		}, nil)
	mocks.Repository.EXPECT().
		PeelToCommit(gomock.Any(), "feat-a").
		Return(git.Hash("abc"), nil)
	// Second call: from MaybeRebuildAndSubmit checking LastPushedHash
	mocks.Store.EXPECT().
		Integration(gomock.Any()).
		Return(&state.IntegrationInfo{
			Name:           "preview",
			LastPushedHash: "", // never pushed
		}, nil)

	require.NoError(t, h.MaybeRebuildAndSubmit(t.Context()))
}

// worktreesSeq returns an iter.Seq2 yielding the given worktree items,
// for mocking GitRepository.Worktrees.
func worktreesSeq(items ...*git.WorktreeListItem) iter.Seq2[*git.WorktreeListItem, error] {
	return func(yield func(*git.WorktreeListItem, error) bool) {
		for _, it := range items {
			if !yield(it, nil) {
				return
			}
		}
	}
}

// expectBorrowableWorktree wires the worktree-safety guard for a
// single-worktree (always borrowable) repository rooted at /repo.
func expectBorrowableWorktree(mocks *handlerMocks) {
	mocks.Worktree.EXPECT().RootDir().Return("/repo").AnyTimes()
	mocks.Repository.EXPECT().
		Worktrees(gomock.Any()).
		Return(worktreesSeq(&git.WorktreeListItem{Path: "/repo"})).
		AnyTimes()
}

// emptyRemoteRefs returns an empty iter.Seq2 for use as a mock return
// value when the remote has no matching branch.
func emptyRemoteRefs() iter.Seq2[git.RemoteRef, error] {
	return func(_ func(git.RemoteRef, error) bool) {}
}

// singleRemoteRef returns an iter.Seq2 that yields ref once.
func singleRemoteRef(ref git.RemoteRef) iter.Seq2[git.RemoteRef, error] {
	return func(yield func(git.RemoteRef, error) bool) {
		yield(ref, nil)
	}
}
