package merge

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/git"
)

func TestExecutePlan_externalMergeWhileWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctrl := gomock.NewController(t)
		pr1 := fakeChangeID("pr-1")
		pr2 := fakeChangeID("pr-2")
		operations := &operationRecorder{}

		repository := forgetest.NewMockRepository(ctrl)
		repository.EXPECT().
			ChangeMergeability(gomock.Any(), pr1).
			Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityWaiting}, nil).
			AnyTimes()
		repository.EXPECT().
			ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
			Return([]forge.ChangeStatus{{State: forge.ChangeOpen}}, nil).
			MaxTimes(1)
		repository.EXPECT().
			ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
			Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil).
			AnyTimes()

		service := NewMockService(ctrl)
		service.EXPECT().VerifyRestacked(gomock.Any(), "feat2").Return(nil).
			MinTimes(0)
		gitRepo := NewMockGitRepository(ctrl)
		gitRepo.EXPECT().PeelToCommit(gomock.Any(), "feat2").
			Return(git.Hash("head2"), nil).MinTimes(0)
		repository.EXPECT().
			ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
			Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: "head2"}}, nil).
			MaxTimes(2)
		repository.EXPECT().
			ChangeMergeability(gomock.Any(), pr2).
			Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityReady}, nil).
			MinTimes(0)
		merge := repository.EXPECT().MergeChange(gomock.Any(), pr2, gomock.Any()).
			DoAndReturn(func(context.Context, forge.ChangeID, forge.MergeChangeOptions) error {
				operations.append("merge pr-2")
				return nil
			}).MinTimes(0)
		repository.EXPECT().
			ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
			Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil).
			After(merge).AnyTimes()

		syncHandler := &recordingSyncHandler{operations: operations}
		h := newTestHandler(t, ctrl, testHandlerOpts{
			forgeRepo: repository,
			service:   service,
			gitRepo:   gitRepo,
			sync:      syncHandler,
		})
		err := h.executePlan(t.Context(), []*mergeItem{
			testPlanEntry("feat1", "main", pr1),
			testPlanEntry("feat2", "feat1", pr2),
		}, mergeExecutionOptions{ReadyTimeout: time.Minute})
		require.NoError(t, err)
		assert.Equal(t, []string{"sync", "merge pr-2", "sync"}, operations.snapshot())
	})
}

func TestMergePlanExecutor_externalMergeWithReadyCommand(t *testing.T) {
	ctrl := gomock.NewController(t)
	pr := fakeChangeID("pr-1")
	repository := forgetest.NewMockRepository(ctrl)
	gomock.InOrder(
		repository.EXPECT().ChangeStatuses(gomock.Any(), []forge.ChangeID{pr}).
			Return([]forge.ChangeStatus{{State: forge.ChangeOpen}}, nil),
		repository.EXPECT().CommandEnvironment(gomock.Any(), pr).Return(nil, nil),
		repository.EXPECT().ChangeStatuses(gomock.Any(), []forge.ChangeID{pr}).
			Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil),
	)
	h := newTestHandler(t, ctrl, testHandlerOpts{forgeRepo: repository})
	executor := &mergePlanExecutor{
		RemoteRepository: repository,
		Repository:       h.Repository,
		Service:          h.Service,
		Restack:          h.Restack,
		Submit:           h.Submit,
		Sync:             h.Sync,
		Progress:         newLogMergeProgress(h.Log),
		MergeRequester:   &forgeMergeRequester{Repository: repository},
		ReadinessChecker: &commandReadinessChecker{
			Runner: &commandRunner{
				Log:        h.Log,
				Repository: repository,
				ForgeID:    "fake",
				Trunk:      "main",
				Runner:     h.ScriptRunner,
			},
			Script: "exit 1",
		},
		Trunk:        "main",
		ReadyTimeout: time.Minute,
		MergeTimeout: time.Second,
		Method:       forge.MergeMethodDefault,
	}
	item := testPlanEntry("feat1", "main", pr)
	require.NoError(t, executor.awaitMergeabilityWithDelay(
		t.Context(), item, time.Minute, time.Millisecond, time.Millisecond,
	))
	require.NoError(t, executor.requestMergeItem(t.Context(), item))
}

func TestExecutePlan_closedWhileWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctrl := gomock.NewController(t)
		pr := fakeChangeID("pr-1")
		repository := forgetest.NewMockRepository(ctrl)
		gomock.InOrder(
			repository.EXPECT().ChangeStatuses(gomock.Any(), []forge.ChangeID{pr}).
				Return([]forge.ChangeStatus{{State: forge.ChangeOpen}}, nil),
			repository.EXPECT().ChangeMergeability(gomock.Any(), pr).
				Return(forge.ChangeMergeability{State: forge.ChangeMergeabilityWaiting}, nil),
			repository.EXPECT().ChangeStatuses(gomock.Any(), []forge.ChangeID{pr}).
				Return([]forge.ChangeStatus{{State: forge.ChangeClosed}}, nil),
		)
		h := newTestHandler(t, ctrl, testHandlerOpts{forgeRepo: repository})
		err := h.executePlan(t.Context(), []*mergeItem{
			testPlanEntry("feat1", "main", pr),
			testPlanEntry("feat2", "feat1", fakeChangeID("pr-2")),
		}, mergeExecutionOptions{ReadyTimeout: time.Minute})
		require.ErrorContains(t, err, "change closed without merging")
	})
}

func TestExecutePlan_externalMergeInNativeRange(t *testing.T) {
	ctrl := gomock.NewController(t)
	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	operations := &operationRecorder{}
	repository := forgetest.NewMockRepository(ctrl)
	repository.EXPECT().ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged, HeadHash: "external-head"}}, nil)
	repository.EXPECT().ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen, HeadHash: "head-2"}}, nil)
	expectMergeWithRecord(repository, pr2, operations)
	service := NewMockService(ctrl)
	service.EXPECT().VerifyRestacked(gomock.Any(), "feat2").Return(nil).Times(2)
	gitRepo := NewMockGitRepository(ctrl)
	gitRepo.EXPECT().PeelToCommit(gomock.Any(), "feat2").Return(git.Hash("head-2"), nil).Times(2)
	rangeRepo := &testStackRepository{
		Repository: repository,
		plans: []forge.MergeRangePlan{&testMergeRangePlan{
			changes: []forge.ChangeID{pr1, pr2},
			merge: func(context.Context, forge.MergeRangeRequest) (forge.MergeOperation, error) {
				t.Fatal("must not request a range containing an already merged change")
				return nil, nil
			},
		}},
	}
	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: rangeRepo,
		service:   service,
		gitRepo:   gitRepo,
		sync:      &recordingSyncHandler{operations: operations},
	})
	require.NoError(t, h.executePlan(t.Context(), []*mergeItem{
		{
			branch:   "feat1",
			base:     "main",
			changeID: pr1,
			headHash: "head-1",
		},
		testPlanEntry("feat2", "feat1", pr2),
	}, mergeExecutionOptions{MergeTimeout: time.Second}))
	assert.Equal(t, []string{"sync", "merge pr-2", "sync"}, operations.snapshot())
}
