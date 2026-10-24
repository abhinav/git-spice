package merge

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
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
	"go.abhg.dev/gs/internal/mergequeue"
)

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
			changes []forge.MergePlanChange,
		) ([]forge.MergeRangePlan, error) {
			assert.Equal(t, []forge.MergePlanChange{
				{Change: pr1},
				{Change: pr2, BaseChange: pr1},
				{Change: pr3, BaseChange: pr2},
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
		Heads: []forge.ChangeHead{
			{
				Change:   pr1,
				HeadHash: "head-1",
			},
			{
				Change:   pr2,
				HeadHash: "head-2",
			},
			{
				Change:   pr3,
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

func TestMergeScheduler_nativeFallbackSerializesWorkspaceChanges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctrl := gomock.NewController(t)
		pr1 := fakeChangeID("pr-1")
		pr2 := fakeChangeID("pr-2")
		pr3 := fakeChangeID("pr-3")

		// feat1 -> feat2 is a native range that falls back to individual merges.
		// Independent feat3 completes while fallback is synchronizing trunk,
		// making the scheduler request its own synchronization barrier.
		mockForge := forgetest.NewMockRepository(ctrl)
		for idx, id := range []fakeChangeID{pr1, pr2} {
			mockForge.EXPECT().
				ChangeStatuses(gomock.Any(), []forge.ChangeID{id}).
				Return([]forge.ChangeStatus{{
					State:    forge.ChangeOpen,
					HeadHash: git.Hash(fmt.Sprintf("head-%d", idx+1)),
				}}, nil).
				Times(idx + 1)
			mockForge.EXPECT().
				ChangeMergeability(gomock.Any(), id).
				Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady}, nil).
				Times(idx + 1)
			mockForge.EXPECT().MergeChange(gomock.Any(), id, gomock.Any()).Return(nil)
			mockForge.EXPECT().
				ChangeStatuses(gomock.Any(), []forge.ChangeID{id}).
				Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)
		}

		syncStarted := make(chan struct{})
		releaseSync := make(chan struct{})
		mockForge.EXPECT().
			ChangeMergeability(gomock.Any(), pr3).
			Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady}, nil)
		mockForge.EXPECT().
			MergeChange(gomock.Any(), pr3, gomock.Any()).
			DoAndReturn(func(context.Context, forge.ChangeID, forge.MergeChangeOptions) error {
				<-syncStarted
				return nil
			})
		mockForge.EXPECT().
			ChangeStatuses(gomock.Any(), []forge.ChangeID{pr3}).
			Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

		rangeRepo := &testStackRepository{
			Repository: mockForge,
			plans: []forge.MergeRangePlan{&testMergeRangePlan{
				changes: []forge.ChangeID{pr1, pr2},
				merge: func(context.Context, forge.MergeRangeRequest) (forge.MergeOperation, error) {
					return nil, forge.ErrUnsupported
				},
			}},
		}
		var syncCalls atomic.Int32
		var syncing, overlapped atomic.Bool
		syncHandler := syncHandlerFunc(func(context.Context, *sync.TrunkOptions) error {
			if syncCalls.Add(1) == 1 {
				syncing.Store(true)
				close(syncStarted)
				<-releaseSync
				syncing.Store(false)
			} else if syncing.Load() {
				overlapped.Store(true)
			}
			return nil
		})
		mockService := NewMockService(ctrl)
		mockGit := NewMockGitRepository(ctrl)
		for idx, branch := range []string{"feat1", "feat2"} {
			mockService.EXPECT().
				VerifyRestacked(gomock.Any(), branch).
				Return(nil).
				Times(idx + 1)
			mockGit.EXPECT().
				PeelToCommit(gomock.Any(), branch).
				Return(git.Hash(fmt.Sprintf("head-%d", idx+1)), nil).
				Times(idx + 1)
		}
		h := newTestHandler(t, ctrl, testHandlerOpts{
			forgeRepo: rangeRepo,
			service:   mockService,
			gitRepo:   mockGit,
			sync:      syncHandler,
		})
		done := make(chan error, 1)
		go func() {
			done <- h.executePlan(t.Context(), []*mergeItem{
				testPlanEntry("feat1", "main", pr1),
				testPlanEntry("feat2", "feat1", pr2),
				testPlanEntry("feat3", "main", pr3),
			}, mergeExecutionOptions{})
		}()

		<-syncStarted
		synctest.Wait()
		assert.False(t, overlapped.Load(), "fallback and scheduler must not synchronize the same workspace concurrently")
		close(releaseSync)
		require.NoError(t, <-done)
		assert.False(t, overlapped.Load())
	})
}

func TestMergeScheduler_nativeFallbackSyncFailureStopsQueue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctrl := gomock.NewController(t)
		prA, prB := fakeChangeID("a"), fakeChangeID("b")
		prX, prY := fakeChangeID("x"), fakeChangeID("y")
		// The native range [a, b] falls back while independent x is merging.
		// After a merges, synchronization fails. Even with FailFast disabled,
		// x must be canceled and its pending above y must never merge.
		var aMerged, xMerged, yMerged atomic.Bool
		mockForge := forgetest.NewMockRepository(ctrl)
		for _, change := range []struct {
			id     fakeChangeID
			head   git.Hash
			merged *atomic.Bool
		}{
			{prA, "head-a", &aMerged},
			{prB, "head-b", new(atomic.Bool)},
			{prX, "head-x", &xMerged},
			{prY, "head-y", &yMerged},
		} {
			mockForge.EXPECT().
				ChangeStatuses(gomock.Any(), []forge.ChangeID{change.id}).
				DoAndReturn(func(context.Context, []forge.ChangeID) ([]forge.ChangeStatus, error) {
					state := forge.ChangeOpen
					if change.merged.Load() {
						state = forge.ChangeMerged
					}
					return []forge.ChangeStatus{{State: state, HeadHash: change.head}}, nil
				}).AnyTimes()
			mockForge.EXPECT().
				ChangeMergeability(gomock.Any(), change.id).
				Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady}, nil).
				AnyTimes()
		}
		mockForge.EXPECT().
			MergeChange(gomock.Any(), prA, gomock.Any()).
			DoAndReturn(func(context.Context, forge.ChangeID, forge.MergeChangeOptions) error {
				aMerged.Store(true)
				return nil
			})
		independentStarted := make(chan struct{})
		releaseIndependent := make(chan struct{})
		mockForge.EXPECT().
			MergeChange(gomock.Any(), prX, gomock.Any()).
			DoAndReturn(func(ctx context.Context, _ forge.ChangeID, _ forge.MergeChangeOptions) error {
				close(independentStarted)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-releaseIndependent:
					xMerged.Store(true)
					return nil
				}
			})
		mockForge.EXPECT().
			MergeChange(gomock.Any(), prY, gomock.Any()).
			DoAndReturn(func(context.Context, forge.ChangeID, forge.MergeChangeOptions) error {
				yMerged.Store(true)
				return nil
			}).AnyTimes()

		mockService := NewMockService(ctrl)
		mockGit := NewMockGitRepository(ctrl)
		for _, branch := range []string{"a", "b", "y"} {
			mockService.EXPECT().VerifyRestacked(gomock.Any(), branch).Return(nil).AnyTimes()
			mockGit.EXPECT().PeelToCommit(gomock.Any(), branch).
				Return(git.Hash("head-"+branch), nil).AnyTimes()
		}
		syncErr := errors.New("cannot synchronize trunk")
		var syncCalls int
		progress := new(recordingMergeProgress)
		executor := &mergePlanExecutor{
			RemoteRepository: mockForge,
			Repository:       mockGit,
			Service:          mockService,
			Restack:          NewMockRestackHandler(ctrl),
			Submit:           NewMockSubmitHandler(ctrl),
			Sync: syncHandlerFunc(func(context.Context, *sync.TrunkOptions) error {
				syncCalls++
				if syncCalls == 1 {
					<-independentStarted
					return syncErr
				}
				return nil
			}),
			Progress:         progress,
			MergeRequester:   &forgeMergeRequester{Repository: mockForge},
			ReadinessChecker: &forgeReadinessChecker{Repository: mockForge},
			MergeRangePlans: []forge.MergeRangePlan{&testMergeRangePlan{
				changes: []forge.ChangeID{prA, prB},
				merge: func(context.Context, forge.MergeRangeRequest) (forge.MergeOperation, error) {
					return nil, forge.ErrUnsupported
				},
			}},
			Trunk:        "main",
			Method:       forge.MergeMethodDefault,
			ReadyTimeout: 0,
			MergeTimeout: time.Second,
		}
		done := make(chan error, 1)
		go func() {
			done <- executor.Execute(t.Context(), []*mergeItem{
				testPlanEntry("a", "main", prA),
				testPlanEntry("b", "a", prB),
				testPlanEntry("x", "main", prX),
				testPlanEntry("y", "x", prY),
			})
		}()
		synctest.Wait()
		close(releaseIndependent)
		err := <-done
		require.ErrorIs(t, err, syncErr)
		_, barrierFailed := errors.AsType[*mergequeue.BarrierError](err)
		assert.True(t, barrierFailed, "fallback synchronization is a queue-wide barrier")
		assert.True(t, aMerged.Load())
		assert.False(t, xMerged.Load(), "in-flight independent work must be canceled")
		assert.False(t, yMerged.Load(), "pending independent work must not merge")
		assert.True(t, progress.seen(mergeProgressMerged, "a"))
		assert.False(t, progress.seen(mergeProgressFailed, "a"))
		assert.True(t, progress.seen(mergeProgressSkipped, "y"))
	})
}

func TestRangeMergeQueueItem_Run_partialFallbackFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	pr3 := fakeChangeID("pr-3")
	plan := []*mergeItem{
		testPlanEntry("feat1", "main", pr1),
		testPlanEntry("feat2", "feat1", pr2),
		testPlanEntry("feat3", "feat2", pr3),
	}
	// The range covers feat1 and feat2. After native refusal, feat1 merges
	// individually but feat2 becomes blocked. Only feat2 may be reported as
	// failed; feat1 remains merged and dependent feat3 must be skipped.
	mockForge := forgetest.NewMockRepository(ctrl)
	mockService := NewMockService(ctrl)
	mockGit := NewMockGitRepository(ctrl)
	for _, member := range []struct {
		id     fakeChangeID
		branch string
		head   git.Hash
		checks int
	}{
		{pr1, "feat1", "head-1", 1},
		{pr2, "feat2", "head-2", 2},
	} {
		mockService.EXPECT().VerifyRestacked(gomock.Any(), member.branch).Return(nil).Times(member.checks)
		mockGit.EXPECT().PeelToCommit(gomock.Any(), member.branch).Return(member.head, nil).Times(member.checks)
		mockForge.EXPECT().
			ChangeStatuses(gomock.Any(), []forge.ChangeID{member.id}).
			Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: member.head}}, nil).
			Times(member.checks)
		mockForge.EXPECT().
			ChangeMergeability(gomock.Any(), member.id).
			Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady}, nil)
	}
	mockForge.EXPECT().MergeChange(gomock.Any(), pr1, gomock.Any()).Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr2).
		Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityBlocked}, nil)
	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		service:   mockService,
		gitRepo:   mockGit,
	})
	progress := new(recordingMergeProgress)
	executor := &mergePlanExecutor{
		RemoteRepository: mockForge,
		Repository:       mockGit,
		Service:          mockService,
		Restack:          h.Restack,
		Submit:           h.Submit,
		Sync:             h.Sync,
		Progress:         progress,
		MergeRequester:   &forgeMergeRequester{Repository: mockForge},
		ReadinessChecker: &forgeReadinessChecker{Repository: mockForge},
		MergeRangePlans: []forge.MergeRangePlan{&testMergeRangePlan{
			changes: []forge.ChangeID{pr1, pr2},
			merge: func(context.Context, forge.MergeRangeRequest) (forge.MergeOperation, error) {
				return nil, forge.ErrUnsupported
			},
		}},
		Trunk:        "main",
		Method:       forge.MergeMethodDefault,
		ReadyTimeout: 0,
		MergeTimeout: time.Second,
	}
	err := executor.Execute(t.Context(), plan)
	require.ErrorContains(t, err, "blocked")
	assert.True(t, progress.seen(mergeProgressMerged, "feat1"))
	assert.False(t, progress.seen(mergeProgressFailed, "feat1"))
	assert.True(t, progress.seen(mergeProgressFailed, "feat2"))
	assert.True(t, progress.seen(mergeProgressSkipped, "feat3"))
}

func TestRangeMergeQueueItem_Run_genuineFailure(t *testing.T) {
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

	queueItem := &rangeMergeQueueItem{
		executor: executor,
		plan:     rangePlan,
		items:    []*mergeItem{item},
		request: forge.MergeRangeRequest{
			Heads: []forge.ChangeHead{{
				Change:   changeID,
				HeadHash: "head-1",
			}},
		},
	}
	err := queueItem.Run(t.Context())
	require.Error(t, err)
	assert.ErrorContains(t, err, "merge range: boom")
}

type testStackRepository struct {
	forge.Repository

	plans           []forge.MergeRangePlan
	planErr         error
	planMergeRanges func(
		context.Context,
		[]forge.MergePlanChange,
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
	changes []forge.MergePlanChange,
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
