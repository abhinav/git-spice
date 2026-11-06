package forgetest

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/fixturetest"
	"go.abhg.dev/gs/internal/forge"
)

// TestStacks exercises creation, extension, and idempotent updates of a
// provider-native stack.
func (s *integrationSuite) TestStacks(t *testing.T, repo forge.StackRepository) {
	bottomBranch := fixturetest.New(s.Fixtures, "bottomBranch", func() string {
		return "stack-bottom-" + randomString(8)
	}).Get(t)
	middleBranch := fixturetest.New(s.Fixtures, "middleBranch", func() string {
		return "stack-middle-" + randomString(8)
	}).Get(t)
	topBranch := fixturetest.New(s.Fixtures, "topBranch", func() string {
		return "stack-top-" + randomString(8)
	}).Get(t)

	if Update() {
		testRepo := NewRepositoryBuilder(t, s.RemoteURL)
		testRepo.CheckoutBranch("main")
		for _, branch := range []string{bottomBranch, middleBranch, topBranch} {
			testRepo.CreateBranch(branch)
			testRepo.CheckoutBranch(branch)
			testRepo.WriteFile(branch+".txt", randomString(32))
			testRepo.AddAllAndCommit("commit for " + branch)
			testRepo.Push(branch)
		}

		t.Cleanup(func() {
			for _, branch := range []string{topBranch, middleBranch, bottomBranch} {
				testRepo.DeleteRemoteBranch(branch)
			}
		})
	}

	bottom, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Native stack bottom " + bottomBranch,
		Body:    "Native stack integration test",
		Base:    "main",
		Head:    bottomBranch,
	})
	require.NoError(t, err, "create bottom change")
	middle, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Native stack middle " + middleBranch,
		Body:    "Native stack integration test",
		Base:    bottomBranch,
		Head:    middleBranch,
	})
	require.NoError(t, err, "create middle change")

	firstUpdate := []forge.StackChange{
		{Change: bottom.ID, BaseBranch: "main"},
		{Change: middle.ID, BaseChange: bottom.ID, BaseBranch: bottomBranch},
	}
	plan, err := repo.PlanStackUpdate(t.Context(), firstUpdate)
	require.NoError(t, err, "plan native stack creation")
	require.NoError(t, plan.Execute(t.Context()), "create native stack")

	top, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Native stack top " + topBranch,
		Body:    "Native stack integration test",
		Base:    middleBranch,
		Head:    topBranch,
	})
	require.NoError(t, err, "create top change")

	completeStack := []forge.StackChange{
		{Change: bottom.ID, BaseBranch: "main"},
		{Change: middle.ID, BaseChange: bottom.ID, BaseBranch: bottomBranch},
		{Change: top.ID, BaseChange: middle.ID, BaseBranch: middleBranch},
	}
	plan, err = repo.PlanStackUpdate(t.Context(), completeStack)
	require.NoError(t, err, "plan native stack extension")
	require.NoError(t, plan.Execute(t.Context()), "extend native stack")
	plan, err = repo.PlanStackUpdate(t.Context(), completeStack)
	require.NoError(t, err, "plan repeated native stack update")
	require.NoError(t, plan.Execute(t.Context()), "repeat native stack update")
}
