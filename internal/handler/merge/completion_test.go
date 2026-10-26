package merge

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
)

func TestAwaitMerged_immediate(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{fakeChangeID("pr-1")}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	executor := new(mergePlanExecutor)
	executor.RemoteRepository = mockRepo
	executor.Progress = new(recordingMergeProgress)
	executor.MergeTimeout = 2 * time.Minute
	err := executor.awaitMerged(t.Context(), []*mergeItem{
		{branch: "feat1", changeID: fakeChangeID("pr-1")},
	}, nil)
	require.NoError(t, err)
}

func TestAwaitMerged_afterPolling(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctrl := gomock.NewController(t)
		ids := []forge.ChangeID{fakeChangeID("pr-1")}
		mockRepo := forgetest.NewMockRepository(ctrl)
		gomock.InOrder(
			mockRepo.EXPECT().
				ChangeStatuses(gomock.Any(), ids).
				Return([]forge.ChangeStatus{{State: forge.ChangeOpen}}, nil),
			mockRepo.EXPECT().
				ChangeStatuses(gomock.Any(), ids).
				Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil),
		)

		executor := new(mergePlanExecutor)
		executor.RemoteRepository = mockRepo
		executor.Progress = new(recordingMergeProgress)
		executor.MergeTimeout = 2 * time.Minute
		err := executor.awaitMerged(t.Context(), []*mergeItem{
			{branch: "feat1", changeID: fakeChangeID("pr-1")},
		}, nil)
		require.NoError(t, err)
	})
}

func TestAwaitMerged_operationAcceptedThenChangesMerge(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctrl := gomock.NewController(t)
		operation := forgetest.NewMockMergeOperation(ctrl)
		ids := []forge.ChangeID{fakeChangeID("pr-1"), fakeChangeID("pr-2")}
		mockRepo := forgetest.NewMockRepository(ctrl)
		gomock.InOrder(
			operation.EXPECT().
				Status(gomock.Any()).
				Return(forge.MergeOperationPending, nil),
			operation.EXPECT().
				Status(gomock.Any()).
				Return(forge.MergeOperationAccepted, nil),
			mockRepo.EXPECT().
				ChangeStatuses(gomock.Any(), ids).
				Return([]forge.ChangeStatus{
					{State: forge.ChangeMerged},
					{State: forge.ChangeOpen},
				}, nil),
			mockRepo.EXPECT().
				ChangeStatuses(gomock.Any(), ids).
				Return([]forge.ChangeStatus{
					{State: forge.ChangeMerged},
					{State: forge.ChangeMerged},
				}, nil),
		)

		executor := new(mergePlanExecutor)
		executor.RemoteRepository = mockRepo
		executor.Progress = new(recordingMergeProgress)
		executor.MergeTimeout = 2 * time.Minute
		err := executor.awaitMerged(t.Context(), []*mergeItem{
			{branch: "feat1", changeID: fakeChangeID("pr-1")},
			{branch: "feat2", changeID: fakeChangeID("pr-2")},
		}, operation)
		require.NoError(t, err)
	})
}

func TestAwaitMerged_respectsMergeTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockRepo := forgetest.NewMockRepository(ctrl)
		mockRepo.EXPECT().
			ChangeStatuses(gomock.Any(), []forge.ChangeID{fakeChangeID("pr-1")}).
			Return([]forge.ChangeStatus{{State: forge.ChangeOpen}}, nil).
			AnyTimes()

		executor := new(mergePlanExecutor)
		executor.RemoteRepository = mockRepo
		executor.Progress = new(recordingMergeProgress)
		executor.MergeTimeout = time.Nanosecond
		err := executor.awaitMerged(t.Context(), []*mergeItem{
			{branch: "feat1", changeID: fakeChangeID("pr-1")},
		}, nil)
		require.EqualError(t, err, "timed out waiting for merge")
	})
}

func TestAwaitMerged_operationAndChangesShareMergeTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctrl := gomock.NewController(t)
		operation := forgetest.NewMockMergeOperation(ctrl)
		mockRepo := forgetest.NewMockRepository(ctrl)
		gomock.InOrder(
			operation.EXPECT().
				Status(gomock.Any()).
				Return(forge.MergeOperationPending, nil),
			operation.EXPECT().
				Status(gomock.Any()).
				Return(forge.MergeOperationAccepted, nil),
			mockRepo.EXPECT().
				ChangeStatuses(gomock.Any(), []forge.ChangeID{fakeChangeID("pr-1")}).
				Return([]forge.ChangeStatus{{State: forge.ChangeOpen}}, nil).
				AnyTimes(),
		)
		executor := new(mergePlanExecutor)
		executor.RemoteRepository = mockRepo
		executor.Progress = new(recordingMergeProgress)
		executor.MergeTimeout = time.Second
		start := time.Now()
		err := executor.awaitMerged(t.Context(), []*mergeItem{
			{branch: "feat1", changeID: fakeChangeID("pr-1")},
		}, operation)
		require.EqualError(t, err, "timed out waiting for merge")
		assert.Equal(t, time.Second, time.Since(start), "acceptance must not reset the merge deadline")
	})
}
