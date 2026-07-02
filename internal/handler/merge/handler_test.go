package merge

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"go.abhg.dev/gs/internal/handler/submit"
	"go.abhg.dev/gs/internal/handler/sync"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/mergequeue"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/spicetest"
	"go.abhg.dev/gs/internal/ui"
)

//go:generate mockgen -destination=mocks_test.go -package=merge -write_package_comment=false -typed=true . Service,Store,RestackHandler,SubmitHandler,SyncHandler,GitRepository

// fakeChangeID is a simple string-based ChangeID for testing.
type fakeChangeID string

func (f fakeChangeID) String() string { return string(f) }

func TestOptions_mergeTimeoutDefault(t *testing.T) {
	var got Options
	parser, err := kong.New(&got)
	require.NoError(t, err)

	_, err = parser.Parse(nil)
	require.NoError(t, err)

	assert.Equal(t, 2*time.Minute, got.MergeTimeout)
}

func TestAwaitMerged_immediate(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeStatuses(
			gomock.Any(),
			[]forge.ChangeID{fakeChangeID("pr-1")},
		).
		Return(
			[]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil,
		)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockRepo,
		logBuffer: nil,
	})

	item := &mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	}
	progress := newLogMergeProgress(silog.Nop())
	executor := newTestMergePlanExecutor(h, progress)

	err := executor.awaitMerged(t.Context(), item)
	require.NoError(t, err)
}

func TestAwaitMerged_afterPolling(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctrl := gomock.NewController(t)

		ids := []forge.ChangeID{fakeChangeID("pr-1")}
		mockRepo := forgetest.NewMockRepository(ctrl)

		// First call: still open.
		mockRepo.EXPECT().
			ChangeStatuses(gomock.Any(), ids).
			Return(
				[]forge.ChangeStatus{{State: forge.ChangeOpen}}, nil,
			)
		// Second call: merged.
		mockRepo.EXPECT().
			ChangeStatuses(gomock.Any(), ids).
			Return(
				[]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil,
			)

		h := newTestHandler(t, ctrl, testHandlerOpts{
			forgeRepo: mockRepo,
			logBuffer: nil,
		})

		item := &mergeItem{
			branch:   "feat1",
			changeID: fakeChangeID("pr-1"),
		}
		progress := newLogMergeProgress(silog.Nop())
		executor := newTestMergePlanExecutor(h, progress)

		err := executor.awaitMerged(t.Context(), item)
		require.NoError(t, err)
	})
}

func TestAwaitMerged_respectsMergeTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctrl := gomock.NewController(t)

		ids := []forge.ChangeID{fakeChangeID("pr-1")}
		mockRepo := forgetest.NewMockRepository(ctrl)
		mockRepo.EXPECT().
			ChangeStatuses(gomock.Any(), ids).
			Return(
				[]forge.ChangeStatus{{State: forge.ChangeOpen}}, nil,
			).
			AnyTimes()

		h := newTestHandler(t, ctrl, testHandlerOpts{
			forgeRepo: mockRepo,
			logBuffer: nil,
		})

		item := &mergeItem{
			branch:   "feat1",
			changeID: fakeChangeID("pr-1"),
		}
		progress := newLogMergeProgress(silog.Nop())
		executor := newTestMergePlanExecutor(h, progress)
		executor.MergeTimeout = time.Nanosecond

		err := executor.awaitMerged(t.Context(), item)
		require.Error(t, err)
		assert.EqualError(t, err, "timed out waiting for merge")
	})
}

func TestAwaitMergeability_ready(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeMergeability(
			gomock.Any(), fakeChangeID("pr-1"),
		).
		Return(mergeability(forge.ChangeMergeabilityReady), nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockRepo,
		logBuffer: nil,
	})

	item := &mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	}
	progress := newLogMergeProgress(silog.Nop())
	executor := newTestMergePlanExecutor(h, progress)

	err := executor.awaitMergeability(t.Context(), item)
	require.NoError(t, err)
}

func TestAwaitMergeability_blocked(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeMergeability(
			gomock.Any(), fakeChangeID("pr-1"),
		).
		Return(
			mergeabilityWithReason(
				forge.ChangeMergeabilityBlocked,
				forge.ChangeMergeabilityReasonChecks,
			),
			nil,
		)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockRepo,
		logBuffer: nil,
	})

	item := &mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	}
	progress := newLogMergeProgress(silog.Nop())
	executor := newTestMergePlanExecutor(h, progress)

	err := executor.awaitMergeability(t.Context(), item)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked: checks")
}

func TestAwaitMergeability_waitingZeroTimeout(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeMergeability(
			gomock.Any(), fakeChangeID("pr-1"),
		).
		Return(mergeability(forge.ChangeMergeabilityWaiting), nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockRepo,
		logBuffer: nil,
	})

	// timeout=0 means fail immediately if pending.
	item := &mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	}
	progress := newLogMergeProgress(silog.Nop())
	executor := newTestMergePlanExecutor(h, progress)

	executor.MergeReadinessTimeout = 0
	err := executor.awaitMergeability(t.Context(), item)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not ready after 0s")
}

func TestAwaitMergeability_waitingThenReady(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockRepo := forgetest.NewMockRepository(ctrl)
		first := mockRepo.EXPECT().
			ChangeMergeability(
				gomock.Any(), fakeChangeID("pr-1"),
			).
			Return(mergeability(forge.ChangeMergeabilityWaiting), nil)
		mockRepo.EXPECT().
			ChangeMergeability(
				gomock.Any(), fakeChangeID("pr-1"),
			).
			Return(mergeability(forge.ChangeMergeabilityReady), nil).
			After(first.Call)

		h := newTestHandler(t, ctrl, testHandlerOpts{
			forgeRepo: mockRepo,
			logBuffer: nil,
		})

		item := &mergeItem{
			branch:   "feat1",
			changeID: fakeChangeID("pr-1"),
		}
		progress := newLogMergeProgress(silog.Nop())
		executor := newTestMergePlanExecutor(h, progress)

		err := executor.awaitMergeability(t.Context(), item)
		require.NoError(t, err)
	})
}

func TestAwaitMergeability_unknown(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeMergeability(
			gomock.Any(), fakeChangeID("pr-1"),
		).
		Return(mergeability(forge.ChangeMergeabilityUnknown), nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockRepo,
		logBuffer: nil,
	})

	item := &mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	}
	progress := newLogMergeProgress(silog.Nop())
	executor := newTestMergePlanExecutor(h, progress)

	err := executor.awaitMergeability(t.Context(), item)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown state")
}

func TestAwaitMergeability_unsupported(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeMergeability(
			gomock.Any(), fakeChangeID("pr-1"),
		).
		Return(mergeability(forge.ChangeMergeabilityUnsupported), nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockRepo,
		logBuffer: nil,
	})

	item := &mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	}
	progress := newLogMergeProgress(silog.Nop())
	executor := newTestMergePlanExecutor(h, progress)

	err := executor.awaitMergeability(t.Context(), item)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown state")
}

func TestExecutePlan_retargets(t *testing.T) {
	ctrl := gomock.NewController(t)
	var logBuffer bytes.Buffer

	mockForge := forgetest.NewMockRepository(ctrl)
	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main").AnyTimes()

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat2").
		Return(&spice.BranchNeedsRestackError{Base: "main"})
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat3").
		Return(&spice.BranchNeedsRestackError{Base: "main"})

	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	pr3 := fakeChangeID("pr-3")

	// Each merge: merge readiness -> merge -> awaitMerged -> sync
	// -> prepare next (except last).
	expectPushedHead(mockForge, pr1, "head1")
	expectMergeItem(mockForge, pr1)
	expectPreparedNext(t, mockForge, pr2, "head2")
	expectMergePreparedItem(mockForge, pr2)
	expectPreparedNext(t, mockForge, pr3, "head3")
	expectMergePreparedItem(mockForge, pr3)

	mockRestack := NewMockRestackHandler(ctrl)
	mockRestack.EXPECT().
		RestackBranch(gomock.Any(), &restack.BranchRequest{
			Branch: "feat2",
		}).
		Return(nil)
	mockRestack.EXPECT().
		RestackBranch(gomock.Any(), &restack.BranchRequest{
			Branch: "feat3",
		}).
		Return(nil)

	mockSubmit := NewMockSubmitHandler(ctrl)
	mockSubmit.EXPECT().
		Submit(gomock.Any(), gomock.Any()).
		DoAndReturn(assertSubmitUpdate(t, "feat2"))
	mockSubmit.EXPECT().
		Submit(gomock.Any(), gomock.Any()).
		DoAndReturn(assertSubmitUpdate(t, "feat3"))

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat2").
		Return(git.Hash("head2"), nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat3").
		Return(git.Hash("head3"), nil)

	mockSync := NewMockSyncHandler(ctrl)
	mockSync.EXPECT().
		SyncTrunk(gomock.Any(), syncTrunkOptions()).
		Return(nil).
		Times(3)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		service:   mockService,
		gitRepo:   mockGit,
		restack:   mockRestack,
		submit:    mockSubmit,
		sync:      mockSync,
		logBuffer: &logBuffer,
	})

	plan := testMergePlan([]*mergeItem{
		{branch: "feat1", changeID: pr1},
		{branch: "feat2", changeID: pr2},
		{branch: "feat3", changeID: pr3},
	})

	err := h.executePlan(t.Context(), plan, mergeExecutionOptions{})
	require.NoError(t, err)

	output := logBuffer.String()
	assert.Contains(t, output, "feat1: merging pr-1: http://example.com/1")
	assert.Contains(t, output, "feat2: merging pr-2: http://example.com/1")
	assert.Contains(t, output, "feat3: merging pr-3: http://example.com/1")
	assert.Contains(t, output, "All 3 change(s) merged")
	assert.NotContains(t, output, "Restacking feat2 after merge")
	assert.NotContains(t, output, "Restacking feat3 after merge")
}

func TestExecutePlan_waitsForPreparedChangeHeadBeforeChecks(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main").AnyTimes()

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat2").
		Return(&spice.BranchNeedsRestackError{Base: "main"})

	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	expectPushedHead(mockForge, pr1, "head1")
	expectMergeItem(mockForge, pr1)

	mockRestack := NewMockRestackHandler(ctrl)
	mockRestack.EXPECT().
		RestackBranch(gomock.Any(), &restack.BranchRequest{
			Branch: "feat2",
		}).
		Return(nil)

	mockSubmit := NewMockSubmitHandler(ctrl)
	mockSubmit.EXPECT().
		Submit(gomock.Any(), gomock.Any()).
		DoAndReturn(assertSubmitUpdate(t, "feat2"))

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat2").
		Return(git.Hash("new-head2"), nil)

	// The submit call can return before the forge's change view catches up
	// to the pushed branch head.
	// A stale merge readiness value at this point belongs to the old head,
	// so the merge loop must wait until the forge reports new-head2
	// before asking whether the change is ready to merge.
	status := mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{
			State:    forge.ChangeOpen,
			HeadHash: git.Hash("new-head2"),
		}}, nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr2).
		Return(mergeability(forge.ChangeMergeabilityReady), nil).
		After(status.Call)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr2, forge.MergeChangeOptions{
			Method:   forge.MergeMethodDefault,
			HeadHash: git.Hash("new-head2"),
		}).
		Return(nil)
	expectMerged(mockForge, pr2)

	mockSync := NewMockSyncHandler(ctrl)
	mockSync.EXPECT().
		SyncTrunk(gomock.Any(), syncTrunkOptions()).
		Return(nil).
		Times(2)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		service:   mockService,
		gitRepo:   mockGit,
		restack:   mockRestack,
		submit:    mockSubmit,
		sync:      mockSync,
	})

	err := h.executePlan(t.Context(), testMergePlan([]*mergeItem{
		{branch: "feat1", changeID: pr1, headHash: git.Hash("head1")},
		{branch: "feat2", changeID: pr2, headHash: git.Hash("old-head2")},
	}), mergeExecutionOptions{})
	require.NoError(t, err)
}

func TestExecutePlan_singleBranch(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main")

	pr1 := fakeChangeID("pr-1")

	// Pre-check: pr-1 already targets main.
	expectPushedHead(mockForge, pr1, "head1")
	expectMergeItem(mockForge, pr1)

	mockSync := NewMockSyncHandler(ctrl)
	mockSync.EXPECT().
		SyncTrunk(gomock.Any(), syncTrunkOptions()).
		Return(nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		sync:      mockSync,
	})

	err := h.executePlan(t.Context(), testMergePlan([]*mergeItem{
		{branch: "feat1", changeID: pr1},
	}), mergeExecutionOptions{})
	require.NoError(t, err)
}

func TestMergeBranch_delegatesToDownstackMerge(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main").AnyTimes()

	mockForge := forgetest.NewMockRepository(ctrl)
	pr1 := fakeChangeID("pr-1")
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen}}, nil)
	expectPushedHead(mockForge, pr1, "head1")
	expectMergeItem(mockForge, pr1)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		CommitAheadBehind(
			gomock.Any(), "origin/feat1", "feat1",
		).
		Return(0, 0, nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat1").
		Return(git.Hash("head1"), nil)

	graph := testBranchGraph(t, []spice.LoadBranchItem{
		testBranch("feat1", "main", pr1),
	})
	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		BranchGraph(gomock.Any(), gomock.Nil()).
		Return(graph, nil)

	mockSync := NewMockSyncHandler(ctrl)
	mockSync.EXPECT().
		SyncTrunk(gomock.Any(), syncTrunkOptions()).
		Return(nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		service:   mockService,
		gitRepo:   mockGit,
		sync:      mockSync,
	})

	err := h.MergeBranch(t.Context(), &BranchMergeRequest{
		Branches: []string{"feat1"},
	})
	require.NoError(t, err)
}

func TestMergeBranch_acceptsMultipleBranches(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1, pr2}).
		Return([]forge.ChangeStatus{
			{State: forge.ChangeOpen},
			{State: forge.ChangeOpen},
		}, nil)

	graph := testBranchGraph(t, []spice.LoadBranchItem{
		testBranchWithoutUpstream("feat1", "main", pr1),
		testBranchWithoutUpstream("feat2", "main", pr2),
	})

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
	})

	plan, err := h.buildPlanFromBranches(t.Context(), mergePlanRequest{
		Graph:    graph,
		Branches: []string{"feat1", "feat2", "feat1"},
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"feat1", "feat2"}, mergePlanBranches(plan))
}

func TestMergeBranch_rejectsSelectedBranchWithoutBase(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		BranchGraph(gomock.Any(), gomock.Nil()).
		Return(testBranchGraph(t, []spice.LoadBranchItem{
			testBranchWithoutUpstream("feat1", "main", fakeChangeID("pr-1")),
			testBranchWithoutUpstream("feat2", "feat1", fakeChangeID("pr-2")),
			testBranchWithoutUpstream("feat3", "feat2", fakeChangeID("pr-3")),
		}), nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		service: mockService,
	})
	err := h.MergeBranch(t.Context(), &BranchMergeRequest{
		Branches: []string{"feat2"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(),
		`branch "feat2" requires selected base "feat1"`)
}

func TestMergeBranch_rejectsSelectedBranchesWithoutPathToTrunk(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		BranchGraph(gomock.Any(), gomock.Nil()).
		Return(testBranchGraph(t, []spice.LoadBranchItem{
			testBranchWithoutUpstream("feat1", "main", fakeChangeID("pr-1")),
			testBranchWithoutUpstream("feat2", "feat1", fakeChangeID("pr-2")),
			testBranchWithoutUpstream("feat3", "feat2", fakeChangeID("pr-3")),
		}), nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		service: mockService,
	})
	err := h.MergeBranch(t.Context(), &BranchMergeRequest{
		Branches: []string{"feat3", "feat2"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(),
		`requires selected base "feat1"`)
}

func TestMergeBranch_acceptsSelectedPathToTrunk(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main").AnyTimes()

	mockForge := forgetest.NewMockRepository(ctrl)
	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	planStatus := mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1, pr2}).
		Return([]forge.ChangeStatus{
			{State: forge.ChangeOpen},
			{State: forge.ChangeOpen},
		}, nil)
	staleBaseStatus := mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen}}, nil).
		After(planStatus.Call)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(mergeability(forge.ChangeMergeabilityReady), nil).
		After(staleBaseStatus)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr1, forge.MergeChangeOptions{}).
		Return(nil)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return(
			[]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil,
		)
	expectPushedHead(mockForge, pr2, "head2")
	expectMergeItem(mockForge, pr2)

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		BranchGraph(gomock.Any(), gomock.Nil()).
		Return(testBranchGraph(t, []spice.LoadBranchItem{
			testBranchWithoutUpstream("feat1", "main", pr1),
			testBranchWithoutUpstream("feat2", "feat1", pr2),
		}), nil)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat2").
		Return(nil)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat2").
		Return(git.Hash("head2"), nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		service:   mockService,
		gitRepo:   mockGit,
	})

	err := h.MergeBranch(t.Context(), &BranchMergeRequest{
		Branches: []string{"feat1", "feat2"},
	})
	require.NoError(t, err)
}

func TestBuildPlan_expandsAndNormalizesDownstackBranches(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	pr3 := fakeChangeID("pr-3")
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1, pr2, pr3}).
		Return([]forge.ChangeStatus{
			{State: forge.ChangeOpen},
			{State: forge.ChangeOpen},
			{State: forge.ChangeOpen},
		}, nil)

	graph := testBranchGraph(t, []spice.LoadBranchItem{
		testBranchWithoutUpstream("feat1", "main", pr1),
		testBranchWithoutUpstream("feat2", "feat1", pr2),
		testBranchWithoutUpstream("feat3", "feat1", pr3),
	})

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
	})
	plan, err := h.buildPlan(t.Context(), &DownstackMergeRequest{
		Branches:    []string{"feat2", "feat3", "feat2"},
		BranchGraph: graph,
		Options: &DownstackMergeOptions{
			NoBranchCheck: true,
		},
	})
	require.NoError(t, err)

	assert.Equal(t,
		[]string{"feat1", "feat2", "feat3"},
		mergePlanBranches(plan),
	)
}

func TestBuildPlan_rejectsUnknownDownstackBranch(t *testing.T) {
	ctrl := gomock.NewController(t)

	h := newTestHandler(t, ctrl, testHandlerOpts{})
	_, err := h.buildPlan(t.Context(), &DownstackMergeRequest{
		Branches: []string{"missing"},
		BranchGraph: testBranchGraph(t, []spice.LoadBranchItem{
			testBranchWithoutUpstream("feat1", "main", fakeChangeID("pr-1")),
		}),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `branch "missing" is not tracked`)
}

func TestMergeStack_includesUpstackDescendants(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main").AnyTimes()

	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	pr3 := fakeChangeID("pr-3")
	graph := testBranchGraph(t, []spice.LoadBranchItem{
		testBranch("feat1", "main", pr1),
		testBranch("feat2", "feat1", pr2),
		testBranch("feat3", "feat1", pr3),
	})

	mockForge := forgetest.NewMockRepository(ctrl)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1, pr2, pr3}).
		Return([]forge.ChangeStatus{
			{State: forge.ChangeOpen},
			{State: forge.ChangeOpen},
			{State: forge.ChangeOpen},
		}, nil)
	expectPushedHead(mockForge, pr1, "head1")
	expectMergeItem(mockForge, pr1)

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		BranchGraph(gomock.Any(), gomock.Nil()).
		Return(graph, nil)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat2").
		Return(nil)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat3").
		Return(nil)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		CommitAheadBehind(gomock.Any(), "origin/feat1", "feat1").
		Return(0, 0, nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat1").
		Return(git.Hash("head1"), nil)
	mockGit.EXPECT().
		CommitAheadBehind(gomock.Any(), "origin/feat2", "feat2").
		Return(0, 0, nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat2").
		Return(git.Hash("head2"), nil).
		Times(2)
	mockGit.EXPECT().
		CommitAheadBehind(gomock.Any(), "origin/feat3", "feat3").
		Return(0, 0, nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat3").
		Return(git.Hash("head3"), nil).
		Times(2)
	expectPushedHead(mockForge, pr2, "head2")
	expectPushedHead(mockForge, pr3, "head3")
	expectMergeItem(mockForge, pr2)
	expectMergeItem(mockForge, pr3)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		service:   mockService,
		gitRepo:   mockGit,
	})

	err := h.MergeStack(t.Context(), &StackMergeRequest{
		Branches: []string{"feat1"},
		Options: &StackMergeOptions{
			NoBranchCheck: true,
		},
	})
	require.NoError(t, err)
}

func TestMergeStack_normalizesContainedScopes(t *testing.T) {
	ctrl := gomock.NewController(t)

	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	pr3 := fakeChangeID("pr-3")
	graph := testBranchGraph(t, []spice.LoadBranchItem{
		testBranchWithoutUpstream("feat1", "main", pr1),
		testBranchWithoutUpstream("feat2", "feat1", pr2),
		testBranchWithoutUpstream("feat3", "feat1", pr3),
	})

	mockForge := forgetest.NewMockRepository(ctrl)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1, pr2, pr3}).
		Return([]forge.ChangeStatus{
			{State: forge.ChangeOpen},
			{State: forge.ChangeOpen},
			{State: forge.ChangeOpen},
		}, nil)

	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main").AnyTimes()
	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		BranchGraph(gomock.Any(), gomock.Nil()).
		Return(graph, nil)
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

	expectMergeItem(mockForge, pr1)
	expectPushedHead(mockForge, pr2, "head2")
	expectMergeItem(mockForge, pr2)
	expectPushedHead(mockForge, pr3, "head3")
	expectMergeItem(mockForge, pr3)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		service:   mockService,
		gitRepo:   mockGit,
	})
	err := h.MergeStack(t.Context(), &StackMergeRequest{
		Branches: []string{"feat1", "feat2"},
		Options: &StackMergeOptions{
			NoBranchCheck: true,
		},
	})
	require.NoError(t, err)
}

func TestMergeStack_ignoresUnsubmittedAboveSubmitted(t *testing.T) {
	ctrl := gomock.NewController(t)
	var logBuffer bytes.Buffer

	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main").AnyTimes()

	pr1 := fakeChangeID("pr-1")
	graph := testBranchGraph(t, []spice.LoadBranchItem{
		testBranch("feat1", "main", pr1),
		{
			Name:           "feat2",
			Base:           "feat1",
			UpstreamBranch: "feat2",
		},
	})

	mockForge := forgetest.NewMockRepository(ctrl)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1}).
		Return([]forge.ChangeStatus{
			{State: forge.ChangeOpen},
		}, nil)
	expectPushedHead(mockForge, pr1, "head1")
	expectMergeItem(mockForge, pr1)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		CommitAheadBehind(gomock.Any(), "origin/feat1", "feat1").
		Return(0, 0, nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat1").
		Return(git.Hash("head1"), nil)

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		BranchGraph(gomock.Any(), gomock.Nil()).
		Return(graph, nil)

	mockSync := NewMockSyncHandler(ctrl)
	mockSync.EXPECT().
		SyncTrunk(gomock.Any(), syncTrunkOptions()).
		Return(nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		service:   mockService,
		gitRepo:   mockGit,
		sync:      mockSync,
		logBuffer: &logBuffer,
	})

	err := h.MergeStack(t.Context(), &StackMergeRequest{
		Branches: []string{"feat1"},
		Options: &StackMergeOptions{
			NoBranchCheck: true,
		},
	})
	require.NoError(t, err)
	assert.Contains(t, logBuffer.String(),
		"feat2: no published change request, skipping")
}

func TestMergeStack_ignoresUnsubmittedBelowSubmitted(t *testing.T) {
	ctrl := gomock.NewController(t)
	var logBuffer bytes.Buffer

	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main").AnyTimes()

	pr2 := fakeChangeID("pr-2")
	graph := testBranchGraph(t, []spice.LoadBranchItem{
		{
			Name:           "feat1",
			Base:           "main",
			UpstreamBranch: "feat1",
		},
		testBranch("feat2", "feat1", pr2),
	})

	mockForge := forgetest.NewMockRepository(ctrl)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen}}, nil)
	expectPushedHead(mockForge, pr2, "head2")
	expectMergeItem(mockForge, pr2)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		CommitAheadBehind(gomock.Any(), "origin/feat2", "feat2").
		Return(0, 0, nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat2").
		Return(git.Hash("head2"), nil).
		Times(2)

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		BranchGraph(gomock.Any(), gomock.Nil()).
		Return(graph, nil)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat2").
		Return(nil)

	mockSync := NewMockSyncHandler(ctrl)
	mockSync.EXPECT().
		SyncTrunk(gomock.Any(), syncTrunkOptions()).
		Return(nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		service:   mockService,
		gitRepo:   mockGit,
		sync:      mockSync,
		logBuffer: &logBuffer,
	})

	err := h.MergeStack(t.Context(), &StackMergeRequest{
		Branches: []string{"feat2"},
		Options: &StackMergeOptions{
			NoBranchCheck: true,
		},
	})
	require.NoError(t, err)
	assert.Contains(t, logBuffer.String(),
		"feat1: no published change request, skipping")
}

func TestMergeStack_allSelectedBranchesUnsubmitted(t *testing.T) {
	ctrl := gomock.NewController(t)
	var logBuffer bytes.Buffer

	graph := testBranchGraph(t, []spice.LoadBranchItem{
		{
			Name:           "feat1",
			Base:           "main",
			UpstreamBranch: "feat1",
		},
		{
			Name:           "feat2",
			Base:           "feat1",
			UpstreamBranch: "feat2",
		},
	})

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		BranchGraph(gomock.Any(), gomock.Nil()).
		Return(graph, nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		service:   mockService,
		logBuffer: &logBuffer,
	})

	err := h.MergeStack(t.Context(), &StackMergeRequest{
		Branches: []string{"feat2"},
		Options: &StackMergeOptions{
			NoBranchCheck: true,
		},
	})
	require.NoError(t, err)
	assert.Contains(t, logBuffer.String(),
		"feat1: no published change request, skipping")
	assert.Contains(t, logBuffer.String(),
		"feat2: no published change request, skipping")
	assert.Contains(t, logBuffer.String(), "No open changes to merge.")
}

func TestMergeStack_passesFailFastToScheduler(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main").AnyTimes()

	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	pr3 := fakeChangeID("pr-3")
	graph := testBranchGraph(t, []spice.LoadBranchItem{
		testBranch("feat1", "main", pr1),
		testBranch("feat2", "feat1", pr2),
		testBranch("feat3", "feat1", pr3),
	})

	mockForge := forgetest.NewMockRepository(ctrl)
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr1, pr2, pr3}).
		Return([]forge.ChangeStatus{
			{State: forge.ChangeOpen},
			{State: forge.ChangeOpen},
			{State: forge.ChangeOpen},
		}, nil)
	expectPushedHead(mockForge, pr1, "head1")
	expectMergeItem(mockForge, pr1)

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		BranchGraph(gomock.Any(), gomock.Nil()).
		Return(graph, nil)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat2").
		Return(nil)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat3").
		Return(nil)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		CommitAheadBehind(gomock.Any(), "origin/feat1", "feat1").
		Return(0, 0, nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat1").
		Return(git.Hash("head1"), nil)
	mockGit.EXPECT().
		CommitAheadBehind(gomock.Any(), "origin/feat2", "feat2").
		Return(0, 0, nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat2").
		Return(git.Hash("head2"), nil).
		Times(2)
	mockGit.EXPECT().
		CommitAheadBehind(gomock.Any(), "origin/feat3", "feat3").
		Return(0, 0, nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat3").
		Return(git.Hash("head3"), nil).
		AnyTimes()
	expectPushedHead(mockForge, pr2, "head2")
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr3}).
		Return([]forge.ChangeStatus{{
			State:    forge.ChangeOpen,
			HeadHash: git.Hash("head3"),
		}}, nil).
		AnyTimes()
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr2).
		Return(mergeability(forge.ChangeMergeabilityBlocked), nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr3).
		Return(mergeability(forge.ChangeMergeabilityReady), nil).
		AnyTimes()
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr3, gomock.Any()).
		Return(nil).
		AnyTimes()
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr3}).
		Return(
			[]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil,
		).
		AnyTimes()

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		service:   mockService,
		gitRepo:   mockGit,
	})

	err := h.MergeStack(t.Context(), &StackMergeRequest{
		Branches: []string{"feat1"},
		Options: &StackMergeOptions{
			NoBranchCheck: true,
			FailFast:      true,
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked")
}

func TestExecutePlan_syncTrunkFailureStopsLoop(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main")

	pr1 := fakeChangeID("pr-1")
	expectMergeItem(mockForge, pr1)

	mockSync := NewMockSyncHandler(ctrl)
	mockSync.EXPECT().
		SyncTrunk(gomock.Any(), syncTrunkOptions()).
		Return(errors.New("sync failed"))

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		sync:      mockSync,
	})

	err := h.executePlan(t.Context(), testMergePlan([]*mergeItem{
		{branch: "feat1", changeID: pr1},
	}), mergeExecutionOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sync trunk")

	var barrierErr *mergequeue.BarrierError
	assert.ErrorAs(t, err, &barrierErr)
	var itemErr *mergequeue.ItemError
	assert.False(t, errors.As(err, &itemErr))
}

func TestExecutePlan_mergeMethod(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main")

	pr1 := fakeChangeID("pr-1")
	status := expectPushedHead(mockForge, pr1, "head1")
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(mergeability(forge.ChangeMergeabilityReady), nil).
		After(status.Call)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr1, forge.MergeChangeOptions{
			Method:   forge.MergeMethodSquash,
			HeadHash: git.Hash("head1"),
		}).
		Return(nil)
	expectMerged(mockForge, pr1)

	mockSync := NewMockSyncHandler(ctrl)
	mockSync.EXPECT().
		SyncTrunk(gomock.Any(), syncTrunkOptions()).
		Return(nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		sync:      mockSync,
	})

	err := h.executePlan(t.Context(), testMergePlan([]*mergeItem{
		{
			branch:   "feat1",
			changeID: pr1,
			headHash: git.Hash("head1"),
		},
	}), mergeExecutionOptions{
		Method: forge.MergeMethodSquash,
	})
	require.NoError(t, err)
}

func TestExecutePlan_mergeCommandRequestsThenAwaitsMerge(t *testing.T) {
	ctrl := gomock.NewController(t)
	var logBuffer bytes.Buffer

	mockForge := forgetest.NewMockRepository(ctrl)
	mockForgeForge := forgetest.NewMockForge(ctrl)
	mockForgeForge.EXPECT().
		ID().
		Return("shamhub").
		AnyTimes()
	mockForge.EXPECT().
		Forge().
		Return(mockForgeForge).
		AnyTimes()
	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().
		Trunk().
		Return("main").
		AnyTimes()

	pr1 := fakeChangeID("pr-1")
	expectPushedHead(mockForge, pr1, "head1")
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(mergeability(forge.ChangeMergeabilityReady), nil)
	mockForge.EXPECT().
		MergeCommandEnvironment(gomock.Any(), pr1).
		Return(map[string]string{
			"GIT_SPICE_SHAMHUB_CHANGE_NUMBER": "1",
			"GIT_SPICE_BRANCH":                "wrong",
		}, nil)
	expectMerged(mockForge, pr1)

	mockSync := NewMockSyncHandler(ctrl)
	mockSync.EXPECT().
		SyncTrunk(gomock.Any(), syncTrunkOptions()).
		Return(nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		sync:      mockSync,
		logBuffer: &logBuffer,
	})

	err := h.executePlan(t.Context(), testMergePlan([]*mergeItem{
		{
			branch:   "feat1",
			changeID: pr1,
			headHash: git.Hash("head1"),
		},
	}), mergeExecutionOptions{
		Command: strings.Join([]string{
			"test \"$GIT_SPICE_FORGE_ID\" = shamhub",
			"test \"$GIT_SPICE_BRANCH\" = feat1",
			"test \"$GIT_SPICE_BASE_BRANCH\" = main",
			"test \"$GIT_SPICE_TRUNK_BRANCH\" = main",
			"test \"$GIT_SPICE_CHANGE_URL\" = http://example.com/1",
			"test \"$GIT_SPICE_HEAD_SHA\" = head1",
			"test \"$GIT_SPICE_SHAMHUB_CHANGE_NUMBER\" = 1",
			"test -z \"$GIT_SPICE_CHANGE_ID\"",
			"echo command stdout",
			"echo command stderr >&2",
		}, "\n"),
	})
	require.NoError(t, err)

	assert.Contains(t, logBuffer.String(), "INF merge: command stdout")
	assert.Contains(t, logBuffer.String(), "INF merge: command stderr")
}

func TestExecutePlan_mergeCommandFailureFailsItem(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	mockForgeForge := forgetest.NewMockForge(ctrl)
	mockForgeForge.EXPECT().
		ID().
		Return("shamhub").
		AnyTimes()
	mockForge.EXPECT().
		Forge().
		Return(mockForgeForge).
		AnyTimes()
	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().
		Trunk().
		Return("main").
		AnyTimes()

	pr1 := fakeChangeID("pr-1")
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr1).
		Return(mergeability(forge.ChangeMergeabilityReady), nil)
	mockForge.EXPECT().
		MergeCommandEnvironment(gomock.Any(), pr1).
		Return(nil, nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
	})

	err := h.executePlan(t.Context(), testMergePlan([]*mergeItem{
		{
			branch:   "feat1",
			changeID: pr1,
		},
	}), mergeExecutionOptions{
		Command: "exit 42",
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "command exited with status 42")
}

func TestExecutePlan_firstItemAlreadyOnTrunk(t *testing.T) {
	ctrl := gomock.NewController(t)
	var logBuffer bytes.Buffer

	mockForge := forgetest.NewMockRepository(ctrl)
	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main")

	pr1 := fakeChangeID("pr-1")

	// Pre-check: pr-1 already targets main.
	expectMergeItem(mockForge, pr1)

	mockSync := NewMockSyncHandler(ctrl)
	mockSync.EXPECT().
		SyncTrunk(gomock.Any(), syncTrunkOptions()).
		Return(nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		sync:      mockSync,
		logBuffer: &logBuffer,
	})

	err := h.executePlan(t.Context(), testMergePlan([]*mergeItem{
		{branch: "feat1", changeID: pr1},
	}), mergeExecutionOptions{})
	require.NoError(t, err)

	assert.NotContains(t,
		logBuffer.String(), "retargeting")
}

func TestLogMergeProgress_deduplicatesRepeatedState(t *testing.T) {
	var logBuffer bytes.Buffer
	progress := newLogMergeProgress(silog.New(&logBuffer, nil))
	item := &mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	}

	progress.Event(mergeProgressEvent{
		Kind: mergeProgressRetargeting,
		Item: item,
		Base: "main",
	})
	progress.Event(mergeProgressEvent{
		Kind: mergeProgressRetargeting,
		Item: item,
		Base: "main",
	})
	progress.Event(mergeProgressEvent{
		Kind: mergeProgressWaitingForMergeability,
		Item: item,
	})
	progress.Event(mergeProgressEvent{
		Kind: mergeProgressWaitingForMergeability,
		Item: item,
	})
	progress.Event(mergeProgressEvent{
		Kind: mergeProgressMerging,
		Item: item,
		URL:  "http://example.com/1",
	})
	progress.Event(mergeProgressEvent{
		Kind: mergeProgressMerging,
		Item: item,
		URL:  "http://example.com/1",
	})
	progress.Event(mergeProgressEvent{
		Kind: mergeProgressFailed,
		Item: item,
	})
	progress.Event(mergeProgressEvent{
		Kind: mergeProgressSkipped,
		Item: item,
	})

	output := logBuffer.String()
	assert.Equal(t, 1, strings.Count(output,
		"feat1: retargeting pr-1 onto main"))
	assert.Equal(t, 1, strings.Count(output,
		"feat1: waiting for merge readiness"))
	assert.Equal(t, 1, strings.Count(output,
		"feat1: merging pr-1: http://example.com/1"))
	assert.NotContains(t, output,
		"feat1: failed")
	assert.Equal(t, 1, strings.Count(output,
		"feat1: skipped"))
}

func TestLogMergeProgress_waitingForMergeIsDebug(t *testing.T) {
	item := &mergeItem{
		branch:   "feat1",
		changeID: fakeChangeID("pr-1"),
	}

	var infoBuffer bytes.Buffer
	infoProgress := newLogMergeProgress(silog.New(&infoBuffer, nil))
	infoProgress.Event(mergeProgressEvent{
		Kind: mergeProgressWaitingForMerge,
		Item: item,
	})
	assert.NotContains(t, infoBuffer.String(),
		"feat1: waiting for merge")

	var debugBuffer bytes.Buffer
	debugProgress := newLogMergeProgress(
		silog.New(&debugBuffer, &silog.Options{
			Level: silog.LevelDebug,
		}),
	)
	debugProgress.Event(mergeProgressEvent{
		Kind: mergeProgressWaitingForMerge,
		Item: item,
	})
	assert.Contains(t, debugBuffer.String(),
		"feat1: waiting for merge")
}

func TestValidateSynced_allInSync(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		CommitAheadBehind(
			gomock.Any(), "origin/feat1", "feat1",
		).
		Return(0, 0, nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat1").
		Return(git.Hash("abc123"), nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		gitRepo: mockGit,
	})

	items := []*mergeItem{
		{
			branch:         "feat1",
			upstreamBranch: "feat1",
		},
	}
	err := h.validateSynced(t.Context(), items)
	require.NoError(t, err)
	assert.Equal(t, git.Hash("abc123"), items[0].headHash)
}

func TestValidateSynced_unpushed(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		CommitAheadBehind(
			gomock.Any(), "origin/feat1", "feat1",
		).
		Return(2, 0, nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		gitRepo: mockGit,
	})

	err := h.validateSynced(t.Context(), []*mergeItem{
		{
			branch:         "feat1",
			upstreamBranch: "feat1",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "feat1 (2 unpushed)")
	assert.Contains(t, err.Error(), "gs branch submit")
	assert.Contains(t, err.Error(), "git reset --hard")
}

func TestValidateSynced_behind(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		CommitAheadBehind(
			gomock.Any(), "origin/feat1", "feat1",
		).
		Return(0, 3, nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		gitRepo: mockGit,
	})

	err := h.validateSynced(t.Context(), []*mergeItem{
		{
			branch:         "feat1",
			upstreamBranch: "feat1",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "feat1 (3 behind remote)")
	assert.Contains(t, err.Error(), "out of sync")
}

func TestValidateSynced_multiple(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		CommitAheadBehind(
			gomock.Any(), "origin/feat1", "feat1",
		).
		Return(1, 0, nil)
	mockGit.EXPECT().
		CommitAheadBehind(
			gomock.Any(), "origin/feat2", "feat2",
		).
		Return(0, 0, nil)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat2").
		Return(git.Hash("def456"), nil)
	mockGit.EXPECT().
		CommitAheadBehind(
			gomock.Any(), "origin/feat3", "feat3",
		).
		Return(0, 2, nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		gitRepo: mockGit,
	})

	err := h.validateSynced(t.Context(), []*mergeItem{
		{branch: "feat1", upstreamBranch: "feat1"},
		{branch: "feat2", upstreamBranch: "feat2"},
		{branch: "feat3", upstreamBranch: "feat3"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "feat1 (1 unpushed)")
	assert.Contains(t, err.Error(), "feat3 (2 behind remote)")
	assert.NotContains(t, err.Error(), "feat2")
}

func TestValidateSynced_errorSkipped(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		CommitAheadBehind(
			gomock.Any(), "origin/feat1", "feat1",
		).
		Return(0, 0, errors.New("not found"))

	h := newTestHandler(t, ctrl, testHandlerOpts{
		gitRepo: mockGit,
	})

	err := h.validateSynced(t.Context(), []*mergeItem{
		{
			branch:         "feat1",
			upstreamBranch: "feat1",
		},
	})
	require.NoError(t, err)
}

// testHandlerOpts configures a Handler for testing.
// All nil fields are filled with no-op defaults.
type testHandlerOpts struct {
	forgeRepo *forgetest.MockRepository
	store     *MockStore
	service   *MockService
	restack   *MockRestackHandler
	submit    *MockSubmitHandler
	sync      SyncHandler
	gitRepo   *MockGitRepository
	logBuffer *bytes.Buffer
}

type testChangeMetadata fakeChangeID

var _ forge.ChangeMetadata = testChangeMetadata("")

func (c testChangeMetadata) ForgeID() string {
	return "fake"
}

func (c testChangeMetadata) ChangeID() forge.ChangeID {
	return fakeChangeID(c)
}

func (c testChangeMetadata) NavigationCommentID() forge.ChangeCommentID {
	return nil
}

func (c testChangeMetadata) SetNavigationCommentID(forge.ChangeCommentID) {}

type testRepositoryID struct{}

var _ forge.RepositoryID = testRepositoryID{}

func (testRepositoryID) String() string {
	return "example/repo"
}

func (testRepositoryID) ChangeURL(forge.ChangeID) string {
	return "http://example.com/1"
}

// newTestHandler builds a Handler with sensible defaults
// for any fields not provided in opts.
func newTestHandler(
	t *testing.T,
	ctrl *gomock.Controller,
	opts testHandlerOpts,
) *Handler {
	t.Helper()

	service := Service(NewMockService(ctrl))
	if opts.service != nil {
		service = opts.service
	}

	return &Handler{
		Log:                testLog(opts.logBuffer),
		View:               ui.NewFileView(io.Discard),
		Remote:             "origin",
		RemoteRepository:   testForgeRepo(ctrl, opts.forgeRepo),
		RemoteRepositoryID: testRepositoryID{},
		Store:              testStore(ctrl, opts.store),
		Service:            service,
		Restack:            testRestack(ctrl, opts.restack),
		Submit:             testSubmit(ctrl, opts.submit),
		Sync:               testSync(opts.sync),
		Repository:         testGitRepo(ctrl, opts.gitRepo),
	}
}

func testLog(buf *bytes.Buffer) *silog.Logger {
	if buf != nil {
		return silog.New(buf, nil)
	}
	return silog.Nop()
}

func newTestMergePlanExecutor(
	h *Handler,
	progress mergeProgress,
) *mergePlanExecutor {
	return &mergePlanExecutor{
		RemoteRepository: h.RemoteRepository,
		Repository:       h.Repository,

		Service: h.Service,
		Restack: h.Restack,
		Submit:  h.Submit,
		Sync:    h.Sync,

		Progress: progress,
		Requester: &directMergeRequester{
			repo:   h.RemoteRepository,
			method: forge.MergeMethodDefault,
		},

		Trunk:                 "main",
		MergeReadinessTimeout: 30 * time.Minute,
		MergeTimeout:          2 * time.Minute,
		Method:                forge.MergeMethodDefault,
	}
}

func testMergePlan(items []*mergeItem) []*mergeItem {
	for idx, item := range items {
		item.base = "main"
		if idx > 0 {
			item.base = items[idx-1].branch
		}
		if item.mergeURL == "" {
			item.mergeURL = testRepositoryID{}.ChangeURL(item.changeID)
		}
	}
	return items
}

func testForgeRepo(
	ctrl *gomock.Controller,
	mock *forgetest.MockRepository,
) forge.Repository {
	if mock != nil {
		return mock
	}
	return forgetest.NewMockRepository(ctrl)
}

func testStore(
	ctrl *gomock.Controller, mock *MockStore,
) Store {
	if mock != nil {
		return mock
	}
	return NewMockStore(ctrl)
}

func testRestack(
	ctrl *gomock.Controller, mock *MockRestackHandler,
) RestackHandler {
	if mock != nil {
		return mock
	}
	return NewMockRestackHandler(ctrl)
}

func testSubmit(
	ctrl *gomock.Controller, mock *MockSubmitHandler,
) SubmitHandler {
	if mock != nil {
		return mock
	}
	return NewMockSubmitHandler(ctrl)
}

type syncHandlerFunc func(context.Context, *sync.TrunkOptions) error

func (f syncHandlerFunc) SyncTrunk(
	ctx context.Context,
	opts *sync.TrunkOptions,
) error {
	return f(ctx, opts)
}

func testSync(syncHandler SyncHandler) SyncHandler {
	if syncHandler != nil {
		return syncHandler
	}
	return syncHandlerFunc(func(context.Context, *sync.TrunkOptions) error {
		return nil
	})
}

func syncTrunkOptions() *sync.TrunkOptions {
	return &sync.TrunkOptions{
		ClosedChanges: sync.ClosedChangesIgnore,
	}
}

func testGitRepo(
	ctrl *gomock.Controller, mock *MockGitRepository,
) GitRepository {
	if mock != nil {
		return mock
	}
	return NewMockGitRepository(ctrl)
}

func testBranchGraph(
	t *testing.T,
	branches []spice.LoadBranchItem,
) *spice.BranchGraph {
	t.Helper()

	return spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
		Trunk:    "main",
		Branches: branches,
	})
}

func testBranch(
	name string,
	base string,
	changeID fakeChangeID,
) spice.LoadBranchItem {
	return spice.LoadBranchItem{
		Name:           name,
		Base:           base,
		Change:         testChangeMetadata(changeID),
		UpstreamBranch: name,
	}
}

func testBranchWithoutUpstream(
	name string,
	base string,
	changeID fakeChangeID,
) spice.LoadBranchItem {
	return spice.LoadBranchItem{
		Name:   name,
		Base:   base,
		Change: testChangeMetadata(changeID),
	}
}

func mergePlanBranches(plan mergePlan) []string {
	branches := make([]string, 0, len(plan.items))
	for _, item := range plan.items {
		branches = append(branches, item.branch)
	}
	return branches
}

// expectMergeItem sets up mock expectations for a full
// merge iteration: ready to merge -> merge -> awaitMerged.
func expectMergeItem(
	mockForge *forgetest.MockRepository,
	id fakeChangeID,
) {
	expectMergeabilityAndMerge(mockForge, id)
	expectMerged(mockForge, id)
}

func expectMergePreparedItem(
	mockForge *forgetest.MockRepository,
	id fakeChangeID,
) {
	mockForge.EXPECT().
		MergeChange(gomock.Any(), id, gomock.Any()).
		Return(nil)

	expectMerged(mockForge, id)
}

func expectMerged(
	mockForge *forgetest.MockRepository,
	id fakeChangeID,
) {
	mockForge.EXPECT().
		ChangeStatuses(gomock.Any(),
			[]forge.ChangeID{id}).
		Return(
			[]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil,
		)
}

func expectPreparedNext(
	t *testing.T,
	mockForge *forgetest.MockRepository,
	id fakeChangeID,
	head git.Hash,
) {
	t.Helper()

	status := expectPushedHead(mockForge, id, head)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), id).
		Return(mergeability(forge.ChangeMergeabilityReady), nil).
		After(status.Call)
}

func expectPushedHead(
	mockForge *forgetest.MockRepository,
	id fakeChangeID,
	head git.Hash,
) *forgetest.MockRepositoryChangeStatusesCall {
	return mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{id}).
		Return([]forge.ChangeStatus{{
			State:    forge.ChangeOpen,
			HeadHash: head,
		}}, nil)
}

func assertSubmitUpdate(
	t *testing.T,
	branch string,
) func(context.Context, *submit.Request) error {
	t.Helper()

	return func(_ context.Context, req *submit.Request) error {
		assert.Equal(t, branch, req.Branch)
		require.NotNil(t, req.Options)
		assert.True(t, req.Options.Publish)
		require.NotNil(t, req.Options.UpdateOnly)
		assert.True(t, *req.Options.UpdateOnly)
		return nil
	}
}

// expectMergeabilityAndMerge sets up mock expectations
// through the forge merge request.
func expectMergeabilityAndMerge(
	mockForge *forgetest.MockRepository,
	id fakeChangeID,
) {
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), id).
		Return(mergeability(forge.ChangeMergeabilityReady), nil)

	mockForge.EXPECT().
		MergeChange(gomock.Any(), id, gomock.Any()).
		Return(nil)
}

func mergeability(
	state forge.ChangeMergeabilityState,
) forge.ChangeMergeability {
	return mergeabilityWithReason(
		state,
		forge.ChangeMergeabilityReasonUnknown,
	)
}

func mergeabilityWithReason(
	state forge.ChangeMergeabilityState,
	reason forge.ChangeMergeabilityReason,
) forge.ChangeMergeability {
	return forge.ChangeMergeability{
		State:  state,
		Reason: reason,
	}
}
