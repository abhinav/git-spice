package merge

import (
	"context"
	"errors"
	"fmt"
	stdsync "sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/sync"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/spicetest"
)

func TestMergePlanExecutor_mergeQueueItems_overlaysNativePlans(t *testing.T) {
	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	pr3 := fakeChangeID("pr-3")
	pr4 := fakeChangeID("pr-4")
	plan := []*mergeItem{
		testPlanEntry("feat1", "main", pr1),
		testPlanEntry("feat2", "feat1", pr2),
		testPlanEntry("feat3", "feat2", pr3),
		testPlanEntry("feat4", "feat2", pr4),
	}

	executor := new(mergePlanExecutor)
	executor.MergeRangePlans = []forge.MergeRangePlan{&testMergeRangePlan{
		changes: []forge.ChangeID{pr1, pr2, pr4},
	}}
	queueItems, err := executor.mergeQueueItems(plan)
	require.NoError(t, err)
	require.Len(t, queueItems, 2)

	assert.Equal(t, "feat4", queueItems[0].ID())
	assert.Empty(t, queueItems[0].Parent())
	assert.Equal(t, "feat3", queueItems[1].ID())
	assert.Equal(t, "feat4", queueItems[1].Parent())

	nativeRange := queueItems[0].(*rangeMergeQueueItem)
	assert.Equal(t, []string{"feat1", "feat2", "feat4"}, []string{
		nativeRange.items[0].branch,
		nativeRange.items[1].branch,
		nativeRange.items[2].branch,
	})
	ordinary := queueItems[1].(*changeMergeQueueItem)
	assert.Equal(t, "feat3", ordinary.item.branch)
}

func TestMergePlanExecutor_mergeQueueItems_rejectsInvalidNativePlans(t *testing.T) {
	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	pr3 := fakeChangeID("pr-3")
	plan := []*mergeItem{
		testPlanEntry("feat1", "main", pr1),
		testPlanEntry("feat2", "feat1", pr2),
		testPlanEntry("feat3", "feat1", pr3),
	}

	t.Run("Empty", func(t *testing.T) {
		executor := new(mergePlanExecutor)
		executor.MergeRangePlans = []forge.MergeRangePlan{&testMergeRangePlan{}}
		_, err := executor.mergeQueueItems(plan)
		require.ErrorContains(t, err, "native merge plan 0 is empty")
	})

	t.Run("UnselectedChange", func(t *testing.T) {
		executor := new(mergePlanExecutor)
		executor.MergeRangePlans = []forge.MergeRangePlan{&testMergeRangePlan{
			changes: []forge.ChangeID{fakeChangeID("pr-4")},
		}}
		_, err := executor.mergeQueueItems(plan)
		require.ErrorContains(t, err, "contains unselected change pr-4")
	})

	t.Run("Overlap", func(t *testing.T) {
		executor := new(mergePlanExecutor)
		executor.MergeRangePlans = []forge.MergeRangePlan{
			&testMergeRangePlan{changes: []forge.ChangeID{pr1, pr2}},
			&testMergeRangePlan{changes: []forge.ChangeID{pr2}},
		}
		_, err := executor.mergeQueueItems(plan)
		require.ErrorContains(t, err, "native merge plan 1 overlaps")
	})

	t.Run("Nonlinear", func(t *testing.T) {
		executor := new(mergePlanExecutor)
		executor.MergeRangePlans = []forge.MergeRangePlan{&testMergeRangePlan{
			changes: []forge.ChangeID{pr2, pr3},
		}}
		_, err := executor.mergeQueueItems(plan)
		require.ErrorContains(t, err, `base branch "feat1", want "feat2"`)
	})
}

func TestExecutePlan_nativePlanningFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	stackRepo := &testStackRepository{
		Repository: forgetest.NewMockRepository(ctrl),
		planErr:    errors.New("boom"),
	}
	h := newTestHandler(t, ctrl, testHandlerOpts{forgeRepo: stackRepo})

	err := h.executePlan(t.Context(), []*mergeItem{
		testPlanEntry("feat1", "main", "pr-1"),
	}, mergeExecutionOptions{})
	require.ErrorContains(t, err, "plan native merge ranges: boom")
}

func TestExecutePlan_nativePlanningUnsupportedFallsBack(t *testing.T) {
	ctrl := gomock.NewController(t)
	changeID := fakeChangeID("pr-1")
	mockForge := forgetest.NewMockRepository(ctrl)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{changeID}).
		Return([]forge.ChangeStatus{{
			State:    forge.ChangeOpen,
			HeadHash: "head-1",
		}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), changeID).
		Return(forge.ChangeMergeability{
			State: forge.ChangeMergeabilityReady,
		}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), changeID, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{changeID}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	stackRepo := &testStackRepository{
		Repository: mockForge,
		planErr:    forge.ErrUnsupported,
	}
	h := newTestHandler(t, ctrl, testHandlerOpts{forgeRepo: stackRepo})
	item := testPlanEntry("feat1", "main", changeID)
	item.headHash = "head-1"

	require.NoError(t, h.executePlan(
		t.Context(),
		[]*mergeItem{item},
		mergeExecutionOptions{},
	))
}

func TestMergeScheduler_nativeLinearPathWithUnselectedDivergence(t *testing.T) {
	ctrl := gomock.NewController(t)
	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	pr3 := fakeChangeID("pr-3")

	mockForge := forgetest.NewMockRepository(ctrl)
	for _, change := range []struct {
		id   fakeChangeID
		head git.Hash
	}{
		{id: pr1, head: "head-1"},
		{id: pr2, head: "head-2"},
		{id: pr3, head: "head-3"},
	} {
		mockForge.EXPECT().
			ChangeStatuses(gomock.Any(), []forge.ChangeID{change.id}).
			Return([]forge.ChangeStatus{{
				State:    forge.ChangeOpen,
				HeadHash: change.head,
			}}, nil)
		mockForge.EXPECT().
			ChangeMergeability(gomock.Any(), change.id).
			Return(forge.ChangeMergeability{
				State: forge.ChangeMergeabilityReady,
			}, nil)
	}
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1, pr2, pr3}).
		Return([]forge.ChangeStatus{
			{State: forge.ChangeMerged},
			{State: forge.ChangeMerged},
			{State: forge.ChangeMerged},
		}, nil)

	var gotRequest forge.MergeRangeRequest
	rangePlan := &testMergeRangePlan{
		changes: []forge.ChangeID{pr1, pr2, pr3},
		merge: func(
			_ context.Context,
			req forge.MergeRangeRequest,
		) (forge.MergeOperation, error) {
			gotRequest = req
			return nil, nil
		},
	}
	rangeRepo := &testStackRepository{
		Repository: mockForge,
		planMergeRanges: func(
			_ context.Context,
			changes []forge.StackChange,
		) ([]forge.MergeRangePlan, error) {
			assert.Equal(t, []forge.StackChange{
				{Change: pr1, BaseBranch: "main"},
				{Change: pr2, BaseChange: pr1, BaseBranch: "feat1"},
				{Change: pr3, BaseChange: pr2, BaseBranch: "feat2"},
			}, changes)
			return []forge.MergeRangePlan{rangePlan}, nil
		},
	}

	mockService := NewMockService(ctrl)
	mockGit := NewMockGitRepository(ctrl)
	for idx, branch := range []string{"feat1", "feat2", "feat3"} {
		mockService.EXPECT().VerifyRestacked(gomock.Any(), branch).Return(nil)
		mockGit.EXPECT().PeelToCommit(gomock.Any(), branch).
			Return(git.Hash(fmt.Sprintf("head-%d", idx+1)), nil)
	}
	mockService.EXPECT().
		BranchGraph(gomock.Any(), nil).
		Return(spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
			Trunk: "main",
			Branches: []spice.LoadBranchItem{
				{
					Name:           "feat1",
					Head:           "head-1",
					Base:           "main",
					Change:         testChangeMetadata(pr1),
					UpstreamBranch: "remote-feat1",
				},
				{
					Name:           "feat2",
					Head:           "head-2",
					Base:           "feat1",
					Change:         testChangeMetadata(pr2),
					UpstreamBranch: "remote-feat2",
				},
				{
					Name:           "feat3",
					Head:           "head-3",
					Base:           "feat2",
					Change:         testChangeMetadata(pr3),
					UpstreamBranch: "remote-feat3",
				},
				{
					Name:           "feat4",
					Head:           "head-4",
					Base:           "feat2",
					Change:         testChangeMetadata("pr-4"),
					UpstreamBranch: "remote-feat4",
				},
			},
		}), nil)

	syncHandler := &recordingSyncHandler{}
	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: rangeRepo,
		service:   mockService,
		gitRepo:   mockGit,
		sync:      syncHandler,
	})
	err := h.executePlan(t.Context(), testMergePlanWithBases(
		testPlanEntry("feat1", "main", pr1),
		testPlanEntry("feat2", "feat1", pr2),
		testPlanEntry("feat3", "feat2", pr3),
	), mergeExecutionOptions{
		Method:       forge.MergeMethodSquash,
		MergeTimeout: time.Second,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, syncHandler.calls)
	assert.Equal(t, forge.MergeRangeRequest{
		Method: forge.MergeMethodSquash,
		Changes: []forge.MergeRangeChange{
			{
				Change:   pr1,
				Base:     "main",
				Head:     "remote-feat1",
				HeadHash: "head-1",
			},
			{
				Change:   pr2,
				Base:     "remote-feat1",
				Head:     "remote-feat2",
				HeadHash: "head-2",
			},
			{
				Change:   pr3,
				Base:     "remote-feat2",
				Head:     "remote-feat3",
				HeadHash: "head-3",
			},
		},
	}, gotRequest)
}

func TestMergeScheduler_nativeRangeUnsupportedFallsBack(t *testing.T) {
	ctrl := gomock.NewController(t)
	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	operations := &operationRecorder{}

	mockForge := forgetest.NewMockRepository(ctrl)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{
			State:    forge.ChangeOpen,
			HeadHash: "head-1",
		}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(forge.ChangeMergeability{
			State: forge.ChangeMergeabilityReady,
		}, nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{
			State:    forge.ChangeOpen,
			HeadHash: "head-2",
		}}, nil).
		Times(2)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr2).
		Return(forge.ChangeMergeability{
			State: forge.ChangeMergeabilityReady,
		}, nil).
		Times(2)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr1, gomock.Any()).
		DoAndReturn(func(
			context.Context,
			forge.ChangeID,
			forge.MergeChangeOptions,
		) error {
			operations.append("merge pr-1")
			return nil
		})
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr2, gomock.Any()).
		DoAndReturn(func(
			context.Context,
			forge.ChangeID,
			forge.MergeChangeOptions,
		) error {
			operations.append("merge pr-2")
			return nil
		})
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	rangePlan := &testMergeRangePlan{
		changes: []forge.ChangeID{pr1, pr2},
		merge: func(
			context.Context,
			forge.MergeRangeRequest,
		) (forge.MergeOperation, error) {
			operations.append("range")
			return nil, forge.ErrUnsupported
		},
	}
	rangeRepo := &testStackRepository{
		Repository: mockForge,
		plans:      []forge.MergeRangePlan{rangePlan},
	}

	mockService := NewMockService(ctrl)
	mockService.EXPECT().VerifyRestacked(gomock.Any(), "feat1").Return(nil)
	mockService.EXPECT().VerifyRestacked(gomock.Any(), "feat2").Return(nil).Times(2)
	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().PeelToCommit(gomock.Any(), "feat1").Return(git.Hash("head-1"), nil)
	mockGit.EXPECT().PeelToCommit(gomock.Any(), "feat2").Return(git.Hash("head-2"), nil).Times(2)
	mockService.EXPECT().
		BranchGraph(gomock.Any(), nil).
		Return(spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
			Trunk: "main",
			Branches: []spice.LoadBranchItem{
				{
					Name:           "feat1",
					Head:           "head-1",
					Base:           "main",
					Change:         testChangeMetadata(pr1),
					UpstreamBranch: "feat1",
				},
				{
					Name:           "feat2",
					Head:           "head-2",
					Base:           "feat1",
					Change:         testChangeMetadata(pr2),
					UpstreamBranch: "feat2",
				},
			},
		}), nil)

	syncHandler := &recordingSyncHandler{operations: operations}
	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: rangeRepo,
		service:   mockService,
		gitRepo:   mockGit,
		sync:      syncHandler,
	})
	err := h.executePlan(t.Context(), testMergePlanWithBases(
		testPlanEntry("feat1", "main", pr1),
		testPlanEntry("feat2", "feat1", pr2),
	), mergeExecutionOptions{MergeTimeout: time.Second})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"range",
		"merge pr-1",
		"sync",
		"merge pr-2",
		"sync",
	}, operations.snapshot())
}

func TestMergePreparedRange_genuineFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	changeID := fakeChangeID("pr-1")
	item := testPlanEntry("feat1", "main", changeID)
	item.headHash = "head-1"

	mockForge := forgetest.NewMockRepository(ctrl)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{changeID}).
		Return([]forge.ChangeStatus{{
			State:    forge.ChangeOpen,
			HeadHash: "head-1",
		}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), changeID).
		Return(forge.ChangeMergeability{
			State: forge.ChangeMergeabilityReady,
		}, nil)
	rangePlan := &testMergeRangePlan{
		changes: []forge.ChangeID{changeID},
		merge: func(
			context.Context,
			forge.MergeRangeRequest,
		) (forge.MergeOperation, error) {
			return nil, errors.New("boom")
		},
	}
	executor := new(mergePlanExecutor)
	executor.RemoteRepository = mockForge
	executor.Progress = new(recordingMergeProgress)
	executor.ReadinessChecker = &forgeReadinessChecker{
		Repository: mockForge,
	}
	executor.ReadyTimeout = time.Second

	completed, err := executor.mergePreparedRange(
		t.Context(),
		rangePlan,
		[]*mergeItem{item},
		forge.MergeRangeRequest{
			Changes: []forge.MergeRangeChange{{
				Change:   changeID,
				Base:     "main",
				Head:     "feat1",
				HeadHash: "head-1",
			}},
		},
	)
	assert.Zero(t, completed)
	require.Error(t, err)
	assert.ErrorContains(t, err, "merge range: boom")
}

func TestMergeScheduler_parentMergeUnlocksIndependentChildren(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main")

	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	pr3 := fakeChangeID("pr-3")
	operations := &operationRecorder{}
	expectMergeWithRecord(mockForge, pr1, operations)

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat2").
		Return(nil)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat3").
		Return(nil)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat2").
		Return(git.Hash("head2"), nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat3").
		Return(git.Hash("head3"), nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head2")}}, nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr3}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head3")}}, nil)
	expectMergeWithRecord(mockForge, pr2, operations)
	expectMergeWithRecord(mockForge, pr3, operations)

	syncHandler := &recordingSyncHandler{operations: operations}
	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		service:   mockService,
		gitRepo:   mockGit,
		sync:      syncHandler,
	})

	err := h.executePlan(t.Context(), testMergePlanWithBases(
		testPlanEntry("feat1", "main", pr1),
		testPlanEntry("feat2", "feat1", pr2),
		testPlanEntry("feat3", "feat1", pr3),
	), mergeExecutionOptions{})
	require.NoError(t, err)

	assert.GreaterOrEqual(t, syncHandler.calls, 2)
	gotOperations := operations.snapshot()
	assert.Equal(t, []string{
		"merge pr-1",
		"sync",
	}, gotOperations[:2])
	assert.Contains(t, gotOperations[2:], "merge pr-2")
	assert.Contains(t, gotOperations[2:], "merge pr-3")
	assert.Contains(t, gotOperations[2:], "sync")
}

func TestMergeScheduler_siblingMergeRequestsRunWhileSyncBlocked(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockForge := forgetest.NewMockRepository(ctrl)
		mockStore := NewMockStore(ctrl)
		mockStore.EXPECT().Trunk().Return("main")

		pr1 := fakeChangeID("pr-1")
		pr2 := fakeChangeID("pr-2")
		pr3 := fakeChangeID("pr-3")
		mockForge.EXPECT().
			ChangeMergeability(gomock.Any(), pr1).
			Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
		mockForge.EXPECT().
			MergeChange(gomock.Any(), pr1, gomock.Any()).
			Return(nil)
		mockForge.EXPECT().
			ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
			Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			VerifyRestacked(gomock.Any(), "feat2").
			Return(nil)
		mockService.EXPECT().
			VerifyRestacked(gomock.Any(), "feat3").
			Return(nil)

		mockGit := NewMockGitRepository(ctrl)
		mockGit.EXPECT().
			PeelToCommit(gomock.Any(), "feat2").
			Return(git.Hash("head2"), nil)
		mockGit.EXPECT().
			PeelToCommit(gomock.Any(), "feat3").
			Return(git.Hash("head3"), nil)
		mockForge.EXPECT().
			ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
			Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head2")}}, nil)
		mockForge.EXPECT().
			ChangeStatuses(gomock.Any(), []forge.ChangeID{pr3}).
			Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head3")}}, nil)

		feat3WaitingForReadiness := make(chan struct{})
		syncBlocked := make(chan struct{})
		feat3MergeRequested := make(chan struct{})

		mockForge.EXPECT().
			ChangeMergeability(gomock.Any(), pr2).
			Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
		mockForge.EXPECT().
			MergeChange(gomock.Any(), pr2, gomock.Any()).
			Return(nil)
		mockForge.EXPECT().
			ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
			DoAndReturn(func(
				ctx context.Context,
				_ []forge.ChangeID,
			) ([]forge.ChangeStatus, error) {
				// Keep pr2 from completing until pr3 has entered Run.
				// Otherwise the sync barrier may correctly block pr3 preparation,
				// and this test would depend on scheduler timing.
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-feat3WaitingForReadiness:
					return []forge.ChangeStatus{{State: forge.ChangeMerged}}, nil
				}
			})

		mockForge.EXPECT().
			ChangeMergeability(gomock.Any(), pr3).
			DoAndReturn(func(
				ctx context.Context,
				_ forge.ChangeID,
			) (forge.ChangeMergeability, error) {
				close(feat3WaitingForReadiness)
				select {
				case <-ctx.Done():
					return forge.ChangeMergeability{}, ctx.Err()
				case <-syncBlocked:
					return forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil
				}
			})
		mockForge.EXPECT().
			MergeChange(gomock.Any(), pr3, gomock.Any()).
			DoAndReturn(func(
				context.Context,
				forge.ChangeID,
				forge.MergeChangeOptions,
			) error {
				close(feat3MergeRequested)
				return nil
			})
		mockForge.EXPECT().
			ChangeStatuses(gomock.Any(), []forge.ChangeID{pr3}).
			Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

		// The second SyncTrunk call belongs to one of the sibling branches.
		// Blocking that call proves the other sibling can still request its merge
		// without waiting for local trunk synchronization to finish.
		syncHandler := &blockingSecondSyncHandler{
			syncBlocked:           syncBlocked,
			waitForSiblingRequest: feat3MergeRequested,
		}
		h := newTestHandler(t, ctrl, testHandlerOpts{
			forgeRepo: mockForge,
			store:     mockStore,
			service:   mockService,
			gitRepo:   mockGit,
			sync:      syncHandler,
		})

		// The timeout is a regression guard for the old gate placement:
		// if SyncTrunk still guards the merge request path,
		// the second sibling merge cannot happen while the first sibling sync
		// is blocked.
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		err := h.executePlan(ctx, testMergePlanWithBases(
			testPlanEntry("feat1", "main", pr1),
			testPlanEntry("feat2", "feat1", pr2),
			testPlanEntry("feat3", "feat1", pr3),
		), mergeExecutionOptions{})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, syncHandler.calls, 2)
	})
}

func TestMergeScheduler_syncBarrierRunsBeforePreparingAboves(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main")

	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	pr3 := fakeChangeID("pr-3")
	pr4 := fakeChangeID("pr-4")
	operations := &operationRecorder{}
	expectMergeWithRecord(mockForge, pr1, operations)

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat2").
		Return(nil)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat3").
		Return(nil)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat4").
		DoAndReturn(func(context.Context, string) error {
			operations.append("prepare feat4")
			return nil
		})

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat2").
		Return(git.Hash("head2"), nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat3").
		Return(git.Hash("head3"), nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat4").
		Return(git.Hash("head4"), nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head2")}}, nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr3}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head3")}}, nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr4}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head4")}}, nil)
	expectMergeWithRecord(mockForge, pr2, operations)
	expectMergeWithRecord(mockForge, pr3, operations)
	expectMergeWithRecord(mockForge, pr4, operations)

	syncHandler := &recordingSyncHandler{operations: operations}
	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		service:   mockService,
		gitRepo:   mockGit,
		sync:      syncHandler,
	})

	err := h.executePlan(t.Context(), testMergePlanWithBases(
		testPlanEntry("feat1", "main", pr1),
		testPlanEntry("feat2", "feat1", pr2),
		testPlanEntry("feat3", "feat1", pr3),
		testPlanEntry("feat4", "feat2", pr4),
	), mergeExecutionOptions{})
	require.NoError(t, err)

	events := operations.snapshot()
	parentMerge := indexOf(t, events, "merge pr-2")
	abovePrepare := indexOf(t, events, "prepare feat4")
	assert.Contains(t, events[parentMerge+1:abovePrepare], "sync")
}

func TestMergeScheduler_siblingContinuesAfterSubtreeFails(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)

	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	pr3 := fakeChangeID("pr-3")
	pr4 := fakeChangeID("pr-4")
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr1, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat2").
		Return(nil)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat3").
		Return(nil)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat2").
		Return(git.Hash("head2"), nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat3").
		Return(git.Hash("head3"), nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head2")}}, nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr3}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head3")}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr2).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityBlocked, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)

	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr3).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr3, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr3}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	progress := &recordingMergeProgress{}
	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		service:   mockService,
		gitRepo:   mockGit,
	})
	err := (&mergePlanExecutor{
		RemoteRepository: h.RemoteRepository,
		Repository:       h.Repository,
		Service:          h.Service,
		Restack:          h.Restack,
		Submit:           h.Submit,
		Sync:             h.Sync,
		Progress:         progress,
		MergeRequester: &forgeMergeRequester{
			Repository: h.RemoteRepository,
			Method:     forge.MergeMethodDefault,
		},
		ReadinessChecker: &forgeReadinessChecker{
			Repository: h.RemoteRepository,
		},
		Trunk:        "main",
		ReadyTimeout: 30 * time.Minute,
		MergeTimeout: 2 * time.Minute,
		Method:       forge.MergeMethodDefault,
	}).Execute(t.Context(), testMergePlanWithBases(
		testPlanEntry("feat1", "main", pr1),
		testPlanEntry("feat2", "feat1", pr2),
		testPlanEntry("feat4", "feat2", pr4),
		testPlanEntry("feat3", "feat1", pr3),
	))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked")
	assert.True(t, progress.seen(mergeProgressFailed, "feat2"))
	assert.True(t, progress.seen(mergeProgressSkipped, "feat4"))
	assert.True(t, progress.seen(mergeProgressMerging, "feat3"))
}

func TestMergeScheduler_missingParentIsQueueRoot(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	pr2 := fakeChangeID("pr-2")

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat2").
		Return(nil)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat2").
		Return(git.Hash("head2"), nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head2")}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr2).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr2, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		service:   mockService,
		gitRepo:   mockGit,
	})
	err := (&mergePlanExecutor{
		RemoteRepository: h.RemoteRepository,
		Repository:       h.Repository,
		Service:          h.Service,
		Restack:          h.Restack,
		Submit:           h.Submit,
		Sync:             h.Sync,
		Progress:         &recordingMergeProgress{},
		MergeRequester: &forgeMergeRequester{
			Repository: h.RemoteRepository,
			Method:     forge.MergeMethodDefault,
		},
		ReadinessChecker: &forgeReadinessChecker{
			Repository: h.RemoteRepository,
		},
		Trunk:        "main",
		ReadyTimeout: 30 * time.Minute,
		MergeTimeout: 2 * time.Minute,
		Method:       forge.MergeMethodDefault,
	}).Execute(t.Context(), testMergePlanWithBases(
		testPlanEntry("feat2", "already-merged-parent", pr2),
	))
	require.NoError(t, err)
}

func TestMergeScheduler_rootWaitsForChangeHeadBeforeReadiness(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	pr1 := fakeChangeID("pr-1")
	status := mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head1")}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil).
		After(status.Call)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr1, forge.MergeChangeOptions{
			Method:   forge.MergeMethodDefault,
			HeadHash: git.Hash("head1"),
		}).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
	})
	err := (&mergePlanExecutor{
		RemoteRepository: h.RemoteRepository,
		Repository:       h.Repository,
		Service:          h.Service,
		Restack:          h.Restack,
		Submit:           h.Submit,
		Sync:             h.Sync,
		Progress:         &recordingMergeProgress{},
		MergeRequester: &forgeMergeRequester{
			Repository: h.RemoteRepository,
			Method:     forge.MergeMethodDefault,
		},
		ReadinessChecker: &forgeReadinessChecker{
			Repository: h.RemoteRepository,
		},
		Trunk:        "main",
		ReadyTimeout: 30 * time.Minute,
		MergeTimeout: 2 * time.Minute,
		Method:       forge.MergeMethodDefault,
	}).Execute(t.Context(), testMergePlanWithBases(&mergeItem{
		branch:   "feat1",
		base:     "main",
		changeID: pr1,
		headHash: git.Hash("head1"),
		mergeURL: testRepositoryID{}.ChangeURL(pr1),
	}))
	require.NoError(t, err)
}

func TestMergeScheduler_restackFailureSkipsSubtree(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)

	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	pr3 := fakeChangeID("pr-3")
	pr4 := fakeChangeID("pr-4")
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr1, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat2").
		Return(errors.New("restack check failed"))
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat3").
		Return(nil)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat3").
		Return(git.Hash("head3"), nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr3}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head3")}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr3).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr3, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr3}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	progress := &recordingMergeProgress{}
	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		service:   mockService,
		gitRepo:   mockGit,
	})
	err := (&mergePlanExecutor{
		RemoteRepository: h.RemoteRepository,
		Repository:       h.Repository,
		Service:          h.Service,
		Restack:          h.Restack,
		Submit:           h.Submit,
		Sync:             h.Sync,
		Progress:         progress,
		MergeRequester: &forgeMergeRequester{
			Repository: h.RemoteRepository,
			Method:     forge.MergeMethodDefault,
		},
		ReadinessChecker: &forgeReadinessChecker{
			Repository: h.RemoteRepository,
		},
		Trunk:        "main",
		ReadyTimeout: 30 * time.Minute,
		MergeTimeout: 2 * time.Minute,
		Method:       forge.MergeMethodDefault,
	}).Execute(t.Context(), testMergePlanWithBases(
		testPlanEntry("feat1", "main", pr1),
		testPlanEntry("feat2", "feat1", pr2),
		testPlanEntry("feat4", "feat2", pr4),
		testPlanEntry("feat3", "feat1", pr3),
	))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "restack check failed")
	assert.True(t, progress.seen(mergeProgressPrepareFailed, "feat2"))
	assert.True(t, progress.seen(mergeProgressFailed, "feat2"))
	assert.True(t, progress.seen(mergeProgressSkipped, "feat4"))
	assert.True(t, progress.seen(mergeProgressMerging, "feat3"))
}

func TestMergeScheduler_failFastSkipsPendingUpstack(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)

	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	pr3 := fakeChangeID("pr-3")
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr1, gomock.Any()).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat2").
		Return(nil)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat2").
		Return(git.Hash("head2"), nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: git.Hash("head2")}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr2).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityBlocked, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)

	progress := &recordingMergeProgress{}
	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		service:   mockService,
		gitRepo:   mockGit,
	})
	executor := &mergePlanExecutor{
		RemoteRepository: h.RemoteRepository,
		Repository:       h.Repository,
		Service:          h.Service,
		Restack:          h.Restack,
		Submit:           h.Submit,
		Sync:             h.Sync,
		Progress:         progress,
		MergeRequester: &forgeMergeRequester{
			Repository: h.RemoteRepository,
			Method:     forge.MergeMethodDefault,
		},
		ReadinessChecker: &forgeReadinessChecker{
			Repository: h.RemoteRepository,
		},
		Trunk:        "main",
		ReadyTimeout: 30 * time.Minute,
		MergeTimeout: 2 * time.Minute,
		Method:       forge.MergeMethodDefault,
		FailFast:     true,
	}

	err := executor.Execute(t.Context(), testMergePlanWithBases(
		testPlanEntry("feat1", "main", pr1),
		testPlanEntry("feat2", "feat1", pr2),
		testPlanEntry("feat3", "feat2", pr3),
	))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked")
	assert.True(t, progress.seen(mergeProgressFailed, "feat2"))
	assert.True(t, progress.seen(mergeProgressSkipped, "feat3"))
}

type recordingSyncHandler struct {
	calls      int
	operations *operationRecorder
}

func (h *recordingSyncHandler) SyncTrunk(
	context.Context,
	*sync.TrunkOptions,
) error {
	h.calls++
	if h.operations != nil {
		h.operations.append("sync")
	}
	return nil
}

type blockingSecondSyncHandler struct {
	// calls tracks sync calls so the helper can block the first sibling sync
	// after the parent branch has already synced.
	calls int

	// syncBlocked is closed when the sibling sync barrier starts blocking.
	syncBlocked chan<- struct{}

	// waitForSiblingRequest lets the blocked sync wait until another
	// already-running sibling requests its merge.
	waitForSiblingRequest <-chan struct{}
}

func (h *blockingSecondSyncHandler) SyncTrunk(
	ctx context.Context,
	_ *sync.TrunkOptions,
) error {
	h.calls++
	if h.calls != 2 {
		return nil
	}

	close(h.syncBlocked)
	select {
	case <-ctx.Done():
		return fmt.Errorf("waiting for sibling merge request: %w", ctx.Err())
	case <-h.waitForSiblingRequest:
		return nil
	}
}

type operationRecorder struct {
	mu    stdsync.Mutex
	items []string
}

func (r *operationRecorder) append(item string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.items = append(r.items, item)
}

func (r *operationRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.items...)
}

type recordingMergeProgress struct {
	mu     stdsync.Mutex
	events []mergeProgressEvent
}

func (p *recordingMergeProgress) Event(event mergeProgressEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.events = append(p.events, event)
}

func (p *recordingMergeProgress) seen(
	kind mergeProgressEventKind,
	branch string,
) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, event := range p.events {
		if event.Kind == kind && event.Item.branch == branch {
			return true
		}
	}
	return false
}

func testPlanEntry(
	branch string,
	base string,
	changeID fakeChangeID,
) *mergeItem {
	return &mergeItem{
		branch:   branch,
		base:     base,
		changeID: changeID,
		mergeURL: testRepositoryID{}.ChangeURL(changeID),
	}
}

func testMergePlanWithBases(items ...*mergeItem) []*mergeItem {
	return items
}

func indexOf(t *testing.T, items []string, target string) int {
	t.Helper()

	for i, item := range items {
		if item == target {
			return i
		}
	}
	t.Fatalf("event %q not found in %v", target, items)
	return 0
}

func expectMergeWithRecord(
	mockForge *forgetest.MockRepository,
	id fakeChangeID,
	operations *operationRecorder,
) {
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), id).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady, Reason: forge.ChangeMergeabilityReasonUnknown}, nil)

	mockForge.EXPECT().
		MergeChange(gomock.Any(), id, gomock.Any()).
		DoAndReturn(func(
			context.Context,
			forge.ChangeID,
			forge.MergeChangeOptions,
		) error {
			operations.append("merge " + id.String())
			return nil
		})

	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{id}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)
}

type testStackRepository struct {
	forge.Repository

	plans           []forge.MergeRangePlan
	planErr         error
	planMergeRanges func(
		context.Context,
		[]forge.StackChange,
	) ([]forge.MergeRangePlan, error)
}

func (*testStackRepository) PlanStackUpdate(
	context.Context,
	[]forge.StackChange,
) (forge.StackUpdatePlan, error) {
	return nil, forge.ErrUnsupported
}

func (r *testStackRepository) PlanMergeRanges(
	ctx context.Context,
	changes []forge.StackChange,
) ([]forge.MergeRangePlan, error) {
	if r.planMergeRanges != nil {
		return r.planMergeRanges(ctx, changes)
	}
	return r.plans, r.planErr
}

type testMergeRangePlan struct {
	changes []forge.ChangeID
	merge   func(
		context.Context,
		forge.MergeRangeRequest,
	) (forge.MergeOperation, error)
}

func (p *testMergeRangePlan) Changes() []forge.ChangeID {
	return p.changes
}

func (p *testMergeRangePlan) Merge(
	ctx context.Context,
	req forge.MergeRangeRequest,
) (forge.MergeOperation, error) {
	return p.merge(ctx, req)
}
