package merge

import (
	"context"
	"errors"
	"testing"

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
	var operations []string

	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr1).
		Return(fakeFindResult("main"), nil)
	expectMergeWithRecord(mockForge, pr1, &operations)

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
		FindChangeByID(gomock.Any(), pr2).
		Return(fakeFindResultWithHead("main", "head2"), nil)
	expectMergeWithRecord(mockForge, pr2, &operations)
	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr3).
		Return(fakeFindResultWithHead("main", "head3"), nil)
	expectMergeWithRecord(mockForge, pr3, &operations)

	syncHandler := &recordingSyncHandler{operations: &operations}
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

	assert.Equal(t, 3, syncHandler.calls)
	assert.Equal(t, []string{
		"merge pr-1",
		"sync",
		"merge pr-2",
		"sync",
		"merge pr-3",
		"sync",
	}, operations)
}

func TestMergeScheduler_siblingContinuesAfterSubtreeFails(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)

	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	pr3 := fakeChangeID("pr-3")
	pr4 := fakeChangeID("pr-4")

	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr1).
		Return(fakeFindResult("main"), nil)
	expectMergeItem(mockForge, pr1)

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
		FindChangeByID(gomock.Any(), pr2).
		Return(fakeFindResultWithHead("main", "head2"), nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr2).
		Return(mergeability(forge.ChangeMergeabilityBlocked), nil)

	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr3).
		Return(fakeFindResultWithHead("main", "head3"), nil)
	expectMergeItem(mockForge, pr3)

	progress := &recordingMergeProgress{}
	err := newTestMergePlanExecutor(
		newTestHandler(t, ctrl, testHandlerOpts{
			forgeRepo: mockForge,
			service:   mockService,
			gitRepo:   mockGit,
		}),
		progress,
	).Execute(t.Context(), testMergePlanWithBases(
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

func TestMergeScheduler_restackFailureSkipsSubtree(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)

	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	pr3 := fakeChangeID("pr-3")
	pr4 := fakeChangeID("pr-4")

	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr1).
		Return(fakeFindResult("main"), nil)
	expectMergeItem(mockForge, pr1)

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
		FindChangeByID(gomock.Any(), pr3).
		Return(fakeFindResultWithHead("main", "head3"), nil)
	expectMergeItem(mockForge, pr3)

	progress := &recordingMergeProgress{}
	err := newTestMergePlanExecutor(
		newTestHandler(t, ctrl, testHandlerOpts{
			forgeRepo: mockForge,
			service:   mockService,
			gitRepo:   mockGit,
		}),
		progress,
	).Execute(t.Context(), testMergePlanWithBases(
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

func TestMergeScheduler_failFastStopsOnFailure(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockForge := forgetest.NewMockRepository(ctrl)

	pr1 := fakeChangeID("pr-1")
	pr2 := fakeChangeID("pr-2")
	pr3 := fakeChangeID("pr-3")

	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr1).
		Return(fakeFindResult("main"), nil)
	expectMergeItem(mockForge, pr1)

	mockService := NewMockService(ctrl)
	mockService.EXPECT().
		VerifyRestacked(gomock.Any(), "feat2").
		Return(nil)

	mockGit := NewMockGitRepository(ctrl)
	mockGit.EXPECT().
		PeelToCommit(gomock.Any(), "feat2").
		Return(git.Hash("head2"), nil)

	mockForge.EXPECT().
		FindChangeByID(gomock.Any(), pr2).
		Return(fakeFindResultWithHead("main", "head2"), nil)
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), pr2).
		Return(mergeability(forge.ChangeMergeabilityBlocked), nil)

	progress := &recordingMergeProgress{}
	executor := newTestMergePlanExecutor(
		newTestHandler(t, ctrl, testHandlerOpts{
			forgeRepo: mockForge,
			service:   mockService,
			gitRepo:   mockGit,
		}),
		progress,
	)
	executor.FailFast = true

	err := executor.Execute(t.Context(), testMergePlanWithBases(
		testPlanEntry("feat1", "main", pr1),
		testPlanEntry("feat2", "feat1", pr2),
		testPlanEntry("feat3", "feat1", pr3),
	))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked")
	assert.False(t, progress.seen(mergeProgressMerging, "feat3"))
}

type recordingSyncHandler struct {
	calls      int
	operations *[]string
}

func (h *recordingSyncHandler) SyncTrunk(
	context.Context,
	*sync.TrunkOptions,
) error {
	h.calls++
	if h.operations != nil {
		*h.operations = append(*h.operations, "sync")
	}
	return nil
}

type recordingMergeProgress struct {
	events []mergeProgressEvent
}

func (p *recordingMergeProgress) Event(event mergeProgressEvent) {
	p.events = append(p.events, event)
}

func (p *recordingMergeProgress) seen(
	kind mergeProgressEventKind,
	branch string,
) bool {
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
	}
}

func testMergePlanWithBases(items ...*mergeItem) []*mergeItem {
	return items
}

func expectMergeWithRecord(
	mockForge *forgetest.MockRepository,
	id fakeChangeID,
	operations *[]string,
) {
	mockForge.EXPECT().
		ChangeMergeability(gomock.Any(), id).
		Return(mergeability(forge.ChangeMergeabilityReady), nil)

	mockForge.EXPECT().
		MergeChange(gomock.Any(), id, gomock.Any()).
		DoAndReturn(func(
			context.Context,
			forge.ChangeID,
			forge.MergeChangeOptions,
		) error {
			*operations = append(*operations, "merge "+id.String())
			return nil
		})

	expectMerged(mockForge, id)
}
