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
)

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
