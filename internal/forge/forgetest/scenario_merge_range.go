package forgetest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/fixturetest"
	"go.abhg.dev/gs/internal/forge"
)

// TestMergeRange exercises validation, asynchronous acceptance, and final
// merged state for a linear range of provider changes.
func (s *integrationSuite) TestMergeRange(
	t *testing.T,
	repo forge.StackRepository,
) {
	bottomBranch := fixturetest.New(s.Fixtures, "mergeRangeBottomBranch", func() string {
		return "merge-range-bottom-" + randomString(8)
	}).Get(t)
	topBranch := fixturetest.New(s.Fixtures, "mergeRangeTopBranch", func() string {
		return "merge-range-top-" + randomString(8)
	}).Get(t)

	if Update() {
		testRepo := NewRepositoryBuilder(t, s.RemoteURL)
		testRepo.CheckoutBranch("main")
		testRepo.CreateBranch(bottomBranch)
		testRepo.CheckoutBranch(bottomBranch)
		testRepo.WriteFile(bottomBranch+".txt", randomString(32))
		testRepo.AddAllAndCommit("commit for merge range bottom")
		testRepo.Push(bottomBranch)

		// The top branch starts at the bottom branch's head so the remote PR
		// relationships describe one fully restacked linear path.
		testRepo.CreateBranch(topBranch)
		testRepo.CheckoutBranch(topBranch)
		testRepo.WriteFile(topBranch+".txt", randomString(32))
		testRepo.AddAllAndCommit("commit for merge range top")
		testRepo.Push(topBranch)

		t.Cleanup(func() {
			testRepo.DeleteRemoteBranch(topBranch)
			testRepo.DeleteRemoteBranch(bottomBranch)
		})
	}

	bottom, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Merge range bottom " + bottomBranch,
		Body:    "Merge range integration test",
		Base:    "main",
		Head:    bottomBranch,
	})
	require.NoError(t, err, "create merge range bottom change")
	top, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Merge range top " + topBranch,
		Body:    "Merge range integration test",
		Base:    bottomBranch,
		Head:    topBranch,
	})
	require.NoError(t, err, "create merge range top change")

	stackChanges := []forge.StackChange{
		{Change: bottom.ID, BaseBranch: "main"},
		{Change: top.ID, BaseChange: bottom.ID, BaseBranch: bottomBranch},
	}
	stackPlan, err := repo.PlanStackUpdate(t.Context(), stackChanges)
	require.NoError(t, err, "plan merge range native stack")
	require.NoError(t, stackPlan.Execute(t.Context()), "create merge range native stack")

	plans, err := repo.PlanMergeRanges(t.Context(), stackChanges)
	require.NoError(t, err, "plan merge range")
	require.Len(t, plans, 1, "planned merge range count")
	assert.Equal(t, []forge.ChangeID{bottom.ID, top.ID}, plans[0].Changes())

	changeIDs := []forge.ChangeID{bottom.ID, top.ID}
	statuses, err := repo.ChangeStatuses(t.Context(), changeIDs)
	require.NoError(t, err, "read merge range head hashes")
	require.Len(t, statuses, len(changeIDs), "merge range status count")

	operation, err := plans[0].Merge(t.Context(), forge.MergeRangeRequest{
		Changes: []forge.MergeRangeChange{
			{
				Change:   bottom.ID,
				Base:     "main",
				Head:     bottomBranch,
				HeadHash: statuses[0].HeadHash,
			},
			{
				Change:   top.ID,
				Base:     bottomBranch,
				Head:     topBranch,
				HeadHash: statuses[1].HeadHash,
			},
		},
	})
	require.NoError(t, err, "start merge range")

	status, err := operation.Status(t.Context())
	require.NoError(t, err, "probe merge range operation")
	assert.Equal(t, forge.MergeOperationPending, status, "merge range operation status")

	// This should be plenty of time--probably.
	// Bump it up if this test is flaky during recording.
	if Update() {
		select {
		case <-time.After(5 * time.Second):

		case <-t.Context().Done():
			require.FailNow(t, "merge range test context canceled")
		}
	}

	status, err = operation.Status(t.Context())
	require.NoError(t, err, "probe merge range operation")
	assert.Equal(t, forge.MergeOperationAccepted, status, "merge range operation status")

	statuses, err = repo.ChangeStatuses(t.Context(), changeIDs)
	require.NoError(t, err, "read merge range change states")
	require.Len(t, statuses, len(changeIDs), "merge range status count")
	for _, status := range statuses {
		assert.Equal(t, forge.ChangeMerged, status.State, "change %d state", status)
	}
}
