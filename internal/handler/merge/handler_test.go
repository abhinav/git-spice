package merge

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"go.abhg.dev/gs/internal/handler/submit"
	"go.abhg.dev/gs/internal/handler/sync"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/spicetest"
	"go.abhg.dev/gs/internal/ui"
)

//go:generate mockgen -destination=mocks_test.go -package=merge -write_package_comment=false -typed=true . Service,Store,RestackHandler,SubmitHandler,SyncHandler,GitRepository

// fakeChangeID is a simple string-based ChangeID for testing.
type fakeChangeID string

func (f fakeChangeID) String() string { return string(f) }

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
}

func TestAwaitChecks_passed(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeChecksState(
			gomock.Any(), fakeChangeID("pr-1"),
		).
		Return(forge.ChecksPassed, nil)

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

	err := executor.awaitChecks(t.Context(), item)
	require.NoError(t, err)
}

func TestAwaitChecks_failed(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeChecksState(
			gomock.Any(), fakeChangeID("pr-1"),
		).
		Return(forge.ChecksFailed, nil)

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

	err := executor.awaitChecks(t.Context(), item)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CI checks failed")
}

func TestAwaitChecks_pendingZeroTimeout(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeChecksState(
			gomock.Any(), fakeChangeID("pr-1"),
		).
		Return(forge.ChecksPending, nil)

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

	executor.BuildTimeout = 0
	err := executor.awaitChecks(t.Context(), item)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CI checks pending")
}

func TestAwaitChecks_pendingThenPassed(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	first := mockRepo.EXPECT().
		ChangeChecksState(
			gomock.Any(), fakeChangeID("pr-1"),
		).
		Return(forge.ChecksPending, nil)
	mockRepo.EXPECT().
		ChangeChecksState(
			gomock.Any(), fakeChangeID("pr-1"),
		).
		Return(forge.ChecksPassed, nil).
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

	err := executor.awaitChecksWithDelay(
		t.Context(),
		item,
		5*time.Second,      // timeout
		1*time.Millisecond, // base delay (fast for test)
		2*time.Millisecond, // max delay
	)
	require.NoError(t, err)
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

	// Pre-check: pr-1 already targets main.
	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr1).
		Return(fakeFindResultWithHead("main", "head1"), nil)
	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr2).
		Return(fakeFindResultWithHead("main", "head2"), nil)
	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr3).
		Return(fakeFindResultWithHead("main", "head3"), nil)

	// Each merge: checks -> merge -> awaitMerged -> sync
	// -> prepare next (except last).
	expectMergeItem(mockForge, pr1)
	expectPreparedNext(t, mockForge, pr2)
	expectMergePreparedItem(mockForge, pr2)
	expectPreparedNext(t, mockForge, pr3)
	expectMergePreparedItem(mockForge, pr3)

	mockRestack := NewMockRestackHandler(ctrl)
	mockRestack.EXPECT().
		RestackBranch(gomock.Any(), "feat2", gomock.Nil()).
		Return(nil)
	mockRestack.EXPECT().
		RestackBranch(gomock.Any(), "feat3", gomock.Nil()).
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

	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr1).
		Return(fakeFindResultWithHead("main", "head1"), nil)
	expectMergeItem(mockForge, pr1)

	mockRestack := NewMockRestackHandler(ctrl)
	mockRestack.EXPECT().
		RestackBranch(gomock.Any(), "feat2").
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
	// A stale ChecksPassed value at this point belongs to the old head,
	// so the merge loop must wait until the forge reports new-head2
	// before asking whether checks passed.
	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr2).
		Return(fakeFindResultWithHead("main", "old-head2"), nil)
	status := mockForge.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{pr2}).
		Return([]forge.ChangeStatus{{
			State:    forge.ChangeOpen,
			HeadHash: git.Hash("new-head2"),
		}}, nil)
	mockForge.EXPECT().
		ChangeChecksState(gomock.Any(), pr2).
		Return(forge.ChecksPassed, nil).
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

func TestExecutePlan_noWait(t *testing.T) {
	ctrl := gomock.NewController(t)
	var logBuffer bytes.Buffer

	mockForge := forgetest.NewMockRepository(ctrl)
	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main")

	pr1 := fakeChangeID("pr-1")

	// Pre-check: pr-1 already targets main.
	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr1).
		Return(fakeFindResultWithHead("main", "head1"), nil)

	expectChecksAndMerge(mockForge, pr1)
	// No ChangesStates polling (awaitMerged skipped).

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		logBuffer: &logBuffer,
	})

	plan := testMergePlan([]*mergeItem{
		{branch: "feat1", changeID: pr1},
	})

	err := h.executePlan(t.Context(), plan, mergeExecutionOptions{
		NoWait: true,
	})
	require.NoError(t, err)

	output := logBuffer.String()
	assert.Contains(t, output, "feat1: merging pr-1: http://example.com/1")
	assert.Contains(t, output, "All 1 change(s) merged")
	assert.NotContains(t, output, "Cleaning up")
}

func TestExecutePlan_singleBranch(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main")

	pr1 := fakeChangeID("pr-1")

	// Pre-check: pr-1 already targets main.
	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr1).
		Return(fakeFindResult("main"), nil)

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
	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr1).
		Return(fakeFindResultWithHead("main", "head1"), nil)
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
		Branch: "feat1",
	})
	require.NoError(t, err)
}

func TestMergeBranch_rejectsBranchNotBasedOnTrunk(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main")

	h := newTestHandler(t, ctrl, testHandlerOpts{
		store: mockStore,
		service: testBranchGraphService(ctrl,
			testBranchGraph(t, []spice.LoadBranchItem{
				testBranch("feat1", "main", fakeChangeID("pr-1")),
				testBranch("feat2", "feat1", fakeChangeID("pr-2")),
			}),
		),
	})

	err := h.MergeBranch(t.Context(), &BranchMergeRequest{
		Branch: "feat2",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(),
		"branch \"feat2\" is based on \"feat1\", not trunk")
	assert.Contains(t, err.Error(), "gs downstack merge --branch feat2")
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
	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr1).
		Return(fakeFindResultWithHead("main", "head1"), nil)
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

	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr2).
		Return(fakeFindResultWithHead("main", "head2"), nil)
	expectMergeItem(mockForge, pr2)
	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr3).
		Return(fakeFindResultWithHead("main", "head3"), nil)
	expectMergeItem(mockForge, pr3)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		service:   mockService,
		gitRepo:   mockGit,
	})

	err := h.MergeStack(t.Context(), &StackMergeRequest{
		Branch: "feat1",
		Options: &StackMergeOptions{
			NoBranchCheck: true,
		},
	})
	require.NoError(t, err)
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
	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr1).
		Return(fakeFindResultWithHead("main", "head1"), nil)
	expectMergeItem(mockForge, pr1)

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		BranchGraph(gomock.Any(), gomock.Nil()).
		Return(graph, nil)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat2").
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
		Return(git.Hash("head3"), nil)

	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr2).
		Return(fakeFindResultWithHead("main", "head2"), nil)
	mockForge.EXPECT().
		ChangeChecksState(gomock.Any(), pr2).
		Return(forge.ChecksFailed, nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
		service:   mockService,
		gitRepo:   mockGit,
	})

	err := h.MergeStack(t.Context(), &StackMergeRequest{
		Branch: "feat1",
		Options: &StackMergeOptions{
			NoBranchCheck: true,
			FailFast:      true,
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CI checks failed")
}

func TestExecutePlan_syncTrunkFailureStopsLoop(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main")

	pr1 := fakeChangeID("pr-1")

	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr1).
		Return(fakeFindResult("main"), nil)

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
}

func TestExecutePlan_mergeMethod(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)
	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main")

	pr1 := fakeChangeID("pr-1")

	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr1).
		Return(fakeFindResultWithHead("main", "head1"), nil)
	mockForge.EXPECT().
		ChangeChecksState(gomock.Any(), pr1).
		Return(forge.ChecksPassed, nil)
	mockForge.EXPECT().
		MergeChange(gomock.Any(), pr1, forge.MergeChangeOptions{
			Method:   forge.MergeMethodSquash,
			HeadHash: git.Hash("head1"),
		}).
		Return(nil)

	h := newTestHandler(t, ctrl, testHandlerOpts{
		forgeRepo: mockForge,
		store:     mockStore,
	})

	err := h.executePlan(t.Context(), testMergePlan([]*mergeItem{
		{
			branch:   "feat1",
			changeID: pr1,
			headHash: git.Hash("head1"),
		},
	}), mergeExecutionOptions{
		Method: forge.MergeMethodSquash,
		NoWait: true,
	})
	require.NoError(t, err)
}

func TestExecutePlan_retargetsStaleFirstItem(t *testing.T) {
	ctrl := gomock.NewController(t)
	var logBuffer bytes.Buffer

	mockForge := forgetest.NewMockRepository(ctrl)
	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main")

	pr1 := fakeChangeID("pr-1")

	// Pre-check: pr-1 has stale base "feature0".
	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr1).
		Return(fakeFindResult("feature0"), nil)

	// Retarget pr-1 to main before merging.
	mockForge.EXPECT().
		EditChange(gomock.Any(), pr1,
			forge.EditChangeOptions{Base: "main"}).
		Return(nil)

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

	output := logBuffer.String()
	assert.Contains(t, output, "feat1: retargeting pr-1 onto main")
	assert.Contains(t, output, "feat1: merging pr-1: http://example.com/1")
}

func TestExecutePlan_firstItemAlreadyOnTrunk(t *testing.T) {
	ctrl := gomock.NewController(t)
	var logBuffer bytes.Buffer

	mockForge := forgetest.NewMockRepository(ctrl)
	mockStore := NewMockStore(ctrl)
	mockStore.EXPECT().Trunk().Return("main")

	pr1 := fakeChangeID("pr-1")

	// Pre-check: pr-1 already targets main.
	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr1).
		Return(fakeFindResult("main"), nil)

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
		Kind: mergeProgressWaitingForChecks,
		Item: item,
	})
	progress.Event(mergeProgressEvent{
		Kind: mergeProgressWaitingForChecks,
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

	output := logBuffer.String()
	assert.Equal(t, 1, strings.Count(output,
		"feat1: retargeting pr-1 onto main"))
	assert.Equal(t, 1, strings.Count(output,
		"feat1: waiting for CI checks"))
	assert.Equal(t, 1, strings.Count(output,
		"feat1: merging pr-1: http://example.com/1"))
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
		Log:              testLog(opts.logBuffer),
		View:             ui.NewFileView(io.Discard),
		Remote:           "origin",
		RemoteRepository: testForgeRepo(ctrl, opts.forgeRepo),
		Store:            testStore(ctrl, opts.store),
		Service:          service,
		Restack:          testRestack(ctrl, opts.restack),
		Submit:           testSubmit(ctrl, opts.submit),
		Sync:             testSync(opts.sync),
		Repository:       testGitRepo(ctrl, opts.gitRepo),
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

		Trunk:        "main",
		BuildTimeout: 30 * time.Minute,
		Method:       forge.MergeMethodDefault,
	}
}

func testMergePlan(items []*mergeItem) []*mergeItem {
	for idx, item := range items {
		item.base = "main"
		if idx > 0 {
			item.base = items[idx-1].branch
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

func testBranchGraphService(
	ctrl *gomock.Controller,
	graph *spice.BranchGraph,
) *MockService {
	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		BranchGraph(gomock.Any(), gomock.Nil()).
		Return(graph, nil)
	return mockService
}

// fakeFindResult returns a minimal FindChangeItem
// with the given base branch name.
func fakeFindResult(
	base string,
) *forge.FindChangeItem {
	return fakeFindResultWithHead(base, "abc123")
}

func fakeFindResultWithHead(
	base string,
	head git.Hash,
) *forge.FindChangeItem {
	return &forge.FindChangeItem{
		ID:       fakeChangeID("find-id"),
		URL:      "http://example.com/1",
		State:    forge.ChangeOpen,
		Subject:  "test change",
		HeadHash: head,
		BaseName: base,
		Draft:    false,
	}
}

// expectMergeItem sets up mock expectations for a full
// merge iteration: checks passed -> merge -> awaitMerged.
func expectMergeItem(
	mockForge *forgetest.MockRepository,
	id fakeChangeID,
) {
	expectChecksAndMerge(mockForge, id)
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
) {
	t.Helper()

	mockForge.EXPECT().
		ChangeChecksState(gomock.Any(), id).
		Return(forge.ChecksPassed, nil)
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

// expectChecksAndMerge sets up mock expectations for
// checks passed + merge (without awaitMerged polling).
func expectChecksAndMerge(
	mockForge *forgetest.MockRepository,
	id fakeChangeID,
) {
	mockForge.EXPECT().
		ChangeChecksState(gomock.Any(), id).
		Return(forge.ChecksPassed, nil)

	mockForge.EXPECT().
		MergeChange(gomock.Any(), id, gomock.Any()).
		Return(nil)
}
