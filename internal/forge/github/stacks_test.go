package github

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.uber.org/mock/gomock"
)

func TestRepository_UpdateStackCreate(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	expectPullRequests(t, gateway, map[int]*github.StackUpdatePullRequest{
		1: newStackPullRequest(nil),
		2: newStackPullRequest(nil),
	})
	gateway.EXPECT().CreatePullRequestStack(gomock.Any(), &github.CreatePullRequestStackInput{
		Owner:        "acme",
		Repo:         "repo",
		PullRequests: []int{1, 2},
	}).Return(nil)

	err := executeStackUpdate(t.Context(), newStackRepository(t, gateway), []forge.StackChange{
		{Change: &PR{Number: 2}, BaseChange: &PR{Number: 1}, BaseBranch: "base"},
		{Change: &PR{Number: 1}, BaseBranch: "base"},
	})
	require.NoError(t, err)
}

func TestRepository_UpdateStackCurrent(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	stack := newPullRequestStack(42, 1, 2)
	expectPullRequests(t, gateway, map[int]*github.StackUpdatePullRequest{
		1: newStackPullRequest(stack),
		2: newStackPullRequest(stack),
	})

	err := executeStackUpdate(t.Context(), newStackRepository(t, gateway), []forge.StackChange{
		{Change: &PR{Number: 1}, BaseBranch: "base"},
		{Change: &PR{Number: 2}, BaseChange: &PR{Number: 1}, BaseBranch: "base"},
	})
	require.NoError(t, err)
}

func TestRepository_UpdateStackExtend(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	stack := newPullRequestStack(42, 1, 2)
	expectPullRequests(t, gateway, map[int]*github.StackUpdatePullRequest{
		1: newStackPullRequest(stack),
		2: newStackPullRequest(stack),
		3: newStackPullRequest(nil),
	})
	gateway.EXPECT().AddPullRequestsToStack(gomock.Any(), &github.AddPullRequestsToStackInput{
		Owner:        "acme",
		Repo:         "repo",
		StackNumber:  42,
		PullRequests: []int{3},
	}).Return(nil)

	err := executeStackUpdate(t.Context(), newStackRepository(t, gateway), []forge.StackChange{
		{Change: &PR{Number: 1}, BaseBranch: "base"},
		{Change: &PR{Number: 2}, BaseChange: &PR{Number: 1}, BaseBranch: "base"},
		{Change: &PR{Number: 3}, BaseChange: &PR{Number: 2}, BaseBranch: "base"},
	})
	require.NoError(t, err)
}

func TestRepository_UpdateStackInsertsIntoLinearStack(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	stack := newPullRequestStack(42, 1, 2)
	expectPullRequests(t, gateway, map[int]*github.StackUpdatePullRequest{
		1: stackPullRequest("PR_1", "main", stack),
		2: stackPullRequest("PR_2", "a", stack),
		3: stackPullRequest("PR_3", "a", nil),
	})
	gateway.EXPECT().UnstackPullRequestStack(gomock.Any(), &github.UnstackPullRequestStackInput{
		Owner:       "acme",
		Repo:        "repo",
		StackNumber: 42,
	}).Return(&github.UnstackPullRequestStackResult{}, nil)
	gateway.EXPECT().UpdatePullRequest(gomock.Any(), &github.UpdatePullRequestInput{
		PullRequestID: "PR_2",
		BaseRefName:   new("c"),
	}).Return(nil)
	gateway.EXPECT().CreatePullRequestStack(gomock.Any(), &github.CreatePullRequestStackInput{
		Owner:        "acme",
		Repo:         "repo",
		PullRequests: []int{1, 3, 2},
	}).Return(nil)

	err := executeStackUpdate(t.Context(), newStackRepository(t, gateway), []forge.StackChange{
		{Change: &PR{Number: 1}, BaseBranch: "main"},
		{Change: &PR{Number: 3}, BaseChange: &PR{Number: 1}, BaseBranch: "a"},
		{Change: &PR{Number: 2}, BaseChange: &PR{Number: 3}, BaseBranch: "c"},
	})
	require.NoError(t, err)
}

func TestRepository_UpdateStackRemovesMember(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	stack := newPullRequestStack(42, 1, 2, 3)
	expectPullRequests(t, gateway, map[int]*github.StackUpdatePullRequest{
		1: stackPullRequest("PR_1", "main", stack),
		3: stackPullRequest("PR_3", "b", stack),
	})
	gateway.EXPECT().UnstackPullRequestStack(gomock.Any(), gomock.Any()).
		Return(&github.UnstackPullRequestStackResult{}, nil)
	gateway.EXPECT().UpdatePullRequest(gomock.Any(), &github.UpdatePullRequestInput{
		PullRequestID: "PR_3",
		BaseRefName:   new("a"),
	}).Return(nil)
	gateway.EXPECT().CreatePullRequestStack(gomock.Any(), &github.CreatePullRequestStackInput{
		Owner:        "acme",
		Repo:         "repo",
		PullRequests: []int{1, 3},
	}).Return(nil)

	err := executeStackUpdate(t.Context(), newStackRepository(t, gateway), []forge.StackChange{
		{Change: &PR{Number: 1}, BaseBranch: "main"},
		{Change: &PR{Number: 3}, BaseChange: &PR{Number: 1}, BaseBranch: "a"},
	})
	require.NoError(t, err)
}

func TestRepository_UpdateStackSplitsExistingStack(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	stack := newPullRequestStack(42, 1, 2)
	expectPullRequests(t, gateway, map[int]*github.StackUpdatePullRequest{
		1: stackPullRequest("PR_1", "main", stack),
		2: stackPullRequest("PR_2", "a", stack),
	})
	gateway.EXPECT().UnstackPullRequestStack(gomock.Any(), gomock.Any()).
		Return(&github.UnstackPullRequestStackResult{}, nil)
	gateway.EXPECT().UpdatePullRequest(gomock.Any(), &github.UpdatePullRequestInput{
		PullRequestID: "PR_2",
		BaseRefName:   new("main"),
	}).Return(nil)

	err := executeStackUpdate(t.Context(), newStackRepository(t, gateway), []forge.StackChange{
		{Change: &PR{Number: 1}, BaseBranch: "main"},
		{Change: &PR{Number: 2}, BaseBranch: "main"},
	})
	require.NoError(t, err)
}

func TestRepository_UpdateStackLeavesLockedStackUnchanged(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	stack := &github.PullRequestStack{
		Number: 42,
		Members: []github.PullRequestStackMember{
			{Number: 1, State: github.PullRequestStateOpen},
			{Number: 2, State: github.PullRequestStateOpen, Locked: true},
		},
	}
	expectPullRequests(t, gateway, map[int]*github.StackUpdatePullRequest{
		1: stackPullRequest("PR_1", "main", stack),
		2: stackPullRequest("PR_2", "a", stack),
		3: stackPullRequest("PR_3", "a", nil),
	})

	var logs strings.Builder
	repo := newStackRepository(t, gateway)
	repo.log = silog.New(&logs, &silog.Options{Level: silog.LevelDebug})
	err := executeStackUpdate(t.Context(), repo, []forge.StackChange{
		{Change: &PR{Number: 1}, BaseBranch: "main"},
		{Change: &PR{Number: 3}, BaseChange: &PR{Number: 1}, BaseBranch: "a"},
		{Change: &PR{Number: 2}, BaseChange: &PR{Number: 3}, BaseBranch: "c"},
	})
	require.NoError(t, err)
	assert.Contains(t, logs.String(),
		"#1: Leaving pull request and its upstack in existing GitHub native stack #42: merged, queued, or auto-merge pull requests prevent restructuring")
}

func TestRepository_UpdateStackLeavesMergedMemberStackUnchanged(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	stack := &github.PullRequestStack{
		Number: 42,
		Members: []github.PullRequestStackMember{
			{Number: 1, State: github.PullRequestStateMerged},
			{Number: 2, State: github.PullRequestStateOpen},
		},
	}
	merged := newStackPullRequest(stack)
	merged.State = github.PullRequestStateMerged
	expectPullRequests(t, gateway, map[int]*github.StackUpdatePullRequest{
		1: merged,
		2: stackPullRequest("PR_2", "a", stack),
		3: stackPullRequest("PR_3", "a", nil),
	})

	var logs strings.Builder
	repo := newStackRepository(t, gateway)
	repo.log = silog.New(&logs, &silog.Options{Level: silog.LevelDebug})
	err := executeStackUpdate(t.Context(), repo, []forge.StackChange{
		{Change: &PR{Number: 1}, BaseBranch: "main"},
		{Change: &PR{Number: 3}, BaseChange: &PR{Number: 1}, BaseBranch: "a"},
		{Change: &PR{Number: 2}, BaseChange: &PR{Number: 3}, BaseBranch: "c"},
	})
	require.NoError(t, err)
	assert.Contains(t, logs.String(),
		"#3: Leaving pull request and its upstack in existing GitHub native stack #42: merged, queued, or auto-merge pull requests prevent restructuring")
}

func TestRepository_PlanStackUpdateDoesNotWrite(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	expectPullRequests(t, gateway, map[int]*github.StackUpdatePullRequest{
		1: newStackPullRequest(nil),
		2: newStackPullRequest(nil),
	})
	writes := 0
	gateway.EXPECT().CreatePullRequestStack(gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, *github.CreatePullRequestStackInput) error {
			writes++
			return nil
		})

	plan, err := newStackRepository(t, gateway).PlanStackUpdate(
		t.Context(),
		[]forge.StackChange{
			{Change: &PR{Number: 1}, BaseBranch: "base"},
			{Change: &PR{Number: 2}, BaseChange: &PR{Number: 1}, BaseBranch: "base"},
		},
	)
	require.NoError(t, err)
	assert.Zero(t, writes)
	require.NoError(t, plan.Execute(t.Context()))
	assert.Equal(t, 1, writes)
}

func TestRepository_UpdateStackDivergent(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	expectPullRequests(t, gateway, map[int]*github.StackUpdatePullRequest{
		1: newStackPullRequest(nil),
		2: newStackPullRequest(nil),
		3: newStackPullRequest(nil),
		4: newStackPullRequest(nil),
		5: newStackPullRequest(nil),
	})
	gateway.EXPECT().CreatePullRequestStack(gomock.Any(), &github.CreatePullRequestStackInput{
		Owner:        "acme",
		Repo:         "repo",
		PullRequests: []int{1, 3, 4},
	}).Return(nil)

	var logs strings.Builder
	repo := newStackRepository(t, gateway)
	repo.log = silog.New(&logs, &silog.Options{Level: silog.LevelDebug})
	err := executeStackUpdate(t.Context(), repo, []forge.StackChange{
		{Change: &PR{Number: 3}, BaseChange: &PR{Number: 1}, BaseBranch: "base"},
		{Change: &PR{Number: 4}, BaseChange: &PR{Number: 3}, BaseBranch: "base"},
		{Change: &PR{Number: 1}, BaseBranch: "base"},
		{Change: &PR{Number: 2}, BaseChange: &PR{Number: 1}, BaseBranch: "base"},
		{Change: &PR{Number: 5}, BaseChange: &PR{Number: 2}, BaseBranch: "base"},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(logs.String(),
		"#2: Leaving pull request and its upstack out of the GitHub native stack: the change tree diverges from the selected linear path"))
	assert.NotContains(t, logs.String(), "#5:")
}

func TestRepository_UpdateStackPrefersExistingStackOverLongerPath(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	stack := newPullRequestStack(42, 1, 2, 4)
	expectPullRequests(t, gateway, map[int]*github.StackUpdatePullRequest{
		1: newStackPullRequest(stack),
		2: newStackPullRequest(stack),
		3: newStackPullRequest(nil),
		4: newStackPullRequest(stack),
		5: newStackPullRequest(nil),
	})

	var logs strings.Builder
	repo := newStackRepository(t, gateway)
	repo.log = silog.New(&logs, &silog.Options{Level: silog.LevelDebug})
	err := executeStackUpdate(t.Context(), repo, []forge.StackChange{
		{Change: &PR{Number: 1}, BaseBranch: "base"},
		{Change: &PR{Number: 2}, BaseChange: &PR{Number: 1}, BaseBranch: "base"},
		{Change: &PR{Number: 3}, BaseChange: &PR{Number: 2}, BaseBranch: "base"},
		{Change: &PR{Number: 4}, BaseChange: &PR{Number: 2}, BaseBranch: "base"},
		{Change: &PR{Number: 5}, BaseChange: &PR{Number: 3}, BaseBranch: "base"},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(logs.String(),
		"#3: Leaving pull request and its upstack out of the GitHub native stack: the change tree diverges from the selected linear path"))
	assert.NotContains(t, logs.String(), "#4:")
	assert.NotContains(t, logs.String(), "incompatible membership")
}

func TestRepository_UpdateStackReplacesIncompatibleLinearStack(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	stack := newPullRequestStack(42, 1, 3)
	expectPullRequests(t, gateway, map[int]*github.StackUpdatePullRequest{
		1: newStackPullRequest(stack),
		2: newStackPullRequest(stack),
	})
	gateway.EXPECT().UnstackPullRequestStack(gomock.Any(), &github.UnstackPullRequestStackInput{
		Owner:       "acme",
		Repo:        "repo",
		StackNumber: 42,
	}).Return(&github.UnstackPullRequestStackResult{}, nil)
	gateway.EXPECT().CreatePullRequestStack(gomock.Any(), &github.CreatePullRequestStackInput{
		Owner:        "acme",
		Repo:         "repo",
		PullRequests: []int{1, 2},
	}).Return(nil)

	repo := newStackRepository(t, gateway)
	err := executeStackUpdate(t.Context(), repo, []forge.StackChange{
		{Change: &PR{Number: 1}, BaseBranch: "base"},
		{Change: &PR{Number: 2}, BaseChange: &PR{Number: 1}, BaseBranch: "base"},
	})
	require.NoError(t, err)
}

func TestRepository_UpdateStackMultipleExistingStacks(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	expectPullRequests(t, gateway, map[int]*github.StackUpdatePullRequest{
		1: newStackPullRequest(newPullRequestStack(42, 1)),
		2: newStackPullRequest(newPullRequestStack(43, 2)),
	})

	var logs strings.Builder
	repo := newStackRepository(t, gateway)
	repo.log = silog.New(&logs, &silog.Options{Level: silog.LevelDebug})
	err := executeStackUpdate(t.Context(), repo, []forge.StackChange{
		{Change: &PR{Number: 1}, BaseBranch: "base"},
		{Change: &PR{Number: 2}, BaseChange: &PR{Number: 1}, BaseBranch: "base"},
	})
	require.NoError(t, err)
	assert.Contains(t, logs.String(),
		"#1: Leaving pull request and its upstack in existing GitHub native stacks: pull requests belong to different native stacks")
}

func TestRepository_UpdateStackUnsupported(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	gateway.EXPECT().CheckPullRequestStacks(gomock.Any(), "acme", "repo").
		Return(github.ErrNotFound)

	err := executeStackUpdate(t.Context(), newStackRepository(t, gateway), []forge.StackChange{
		{Change: &PR{Number: 1}, BaseBranch: "base"},
		{Change: &PR{Number: 2}, BaseChange: &PR{Number: 1}, BaseBranch: "base"},
	})
	require.ErrorIs(t, err, forge.ErrUnsupported)
	assert.ErrorIs(t, err, github.ErrNotFound)
}

func TestRepository_UpdateStackWriteNotFoundIsFailure(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	expectPullRequests(t, gateway, map[int]*github.StackUpdatePullRequest{
		1: newStackPullRequest(nil),
		2: newStackPullRequest(nil),
	})
	gateway.EXPECT().CreatePullRequestStack(gomock.Any(), gomock.Any()).
		Return(github.ErrNotFound)

	err := executeStackUpdate(t.Context(), newStackRepository(t, gateway), []forge.StackChange{
		{Change: &PR{Number: 1}, BaseBranch: "base"},
		{Change: &PR{Number: 2}, BaseChange: &PR{Number: 1}, BaseBranch: "base"},
	})
	require.ErrorIs(t, err, github.ErrNotFound)
	assert.NotErrorIs(t, err, forge.ErrUnsupported)
}

func TestRepository_UpdateStackMissingChange(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	expectPullRequests(t, gateway, map[int]*github.StackUpdatePullRequest{1: nil})

	err := executeStackUpdate(t.Context(), newStackRepository(t, gateway), []forge.StackChange{
		{Change: &PR{Number: 1}, BaseBranch: "base"},
	})
	require.ErrorIs(t, err, forge.ErrNotFound)
}

func TestRepository_UpdateStackLookupFailure(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	gateway.EXPECT().CheckPullRequestStacks(gomock.Any(), "acme", "repo").Return(nil)
	gateway.EXPECT().PullRequestsForStackUpdate(
		gomock.Any(),
		"acme",
		"repo",
		[]int{1},
	).Return(nil, github.ErrForbidden)

	err := executeStackUpdate(t.Context(), newStackRepository(t, gateway), []forge.StackChange{
		{Change: &PR{Number: 1}, BaseBranch: "base"},
	})
	require.ErrorIs(t, err, github.ErrForbidden)
}

func TestRepository_UpdateStackCanceled(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	gateway.EXPECT().CheckPullRequestStacks(gomock.Any(), "acme", "repo").Return(nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := executeStackUpdate(ctx, newStackRepository(t, gateway), []forge.StackChange{
		{Change: &PR{Number: 1}, BaseBranch: "base"},
	})
	require.ErrorIs(t, err, context.Canceled)
}

func TestRepository_UpdateStackMultipleRoots(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	expectPullRequests(t, gateway, map[int]*github.StackUpdatePullRequest{
		1:  newStackPullRequest(nil),
		2:  newStackPullRequest(nil),
		10: newStackPullRequest(nil),
		11: newStackPullRequest(nil),
	})
	gateway.EXPECT().CreatePullRequestStack(gomock.Any(), &github.CreatePullRequestStackInput{
		Owner:        "acme",
		Repo:         "repo",
		PullRequests: []int{1, 2},
	}).Return(nil)
	gateway.EXPECT().CreatePullRequestStack(gomock.Any(), &github.CreatePullRequestStackInput{
		Owner:        "acme",
		Repo:         "repo",
		PullRequests: []int{10, 11},
	}).Return(nil)

	err := executeStackUpdate(t.Context(), newStackRepository(t, gateway), []forge.StackChange{
		{Change: &PR{Number: 11}, BaseChange: &PR{Number: 10}, BaseBranch: "base"},
		{Change: &PR{Number: 2}, BaseChange: &PR{Number: 1}, BaseBranch: "base"},
		{Change: &PR{Number: 10}, BaseBranch: "base"},
		{Change: &PR{Number: 1}, BaseBranch: "base"},
	})
	require.NoError(t, err)
}

func TestRepository_UpdateStackContinuesAfterFailure(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	expectPullRequests(t, gateway, map[int]*github.StackUpdatePullRequest{
		1:  newStackPullRequest(nil),
		2:  newStackPullRequest(nil),
		10: newStackPullRequest(nil),
		11: newStackPullRequest(nil),
	})
	gateway.EXPECT().CreatePullRequestStack(gomock.Any(), &github.CreatePullRequestStackInput{
		Owner:        "acme",
		Repo:         "repo",
		PullRequests: []int{1, 2},
	}).Return(github.ErrForbidden)
	gateway.EXPECT().CreatePullRequestStack(gomock.Any(), &github.CreatePullRequestStackInput{
		Owner:        "acme",
		Repo:         "repo",
		PullRequests: []int{10, 11},
	}).Return(nil)

	err := executeStackUpdate(t.Context(), newStackRepository(t, gateway), []forge.StackChange{
		{Change: &PR{Number: 1}, BaseBranch: "base"},
		{Change: &PR{Number: 2}, BaseChange: &PR{Number: 1}, BaseBranch: "base"},
		{Change: &PR{Number: 10}, BaseBranch: "base"},
		{Change: &PR{Number: 11}, BaseChange: &PR{Number: 10}, BaseBranch: "base"},
	})
	require.ErrorIs(t, err, github.ErrForbidden)
}

func TestRepository_UpdateStackReconnectsAboveMergedChange(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	merged := newStackPullRequest(nil)
	merged.State = github.PullRequestStateMerged
	expectPullRequests(t, gateway, map[int]*github.StackUpdatePullRequest{
		1: merged,
		2: newStackPullRequest(nil),
		3: newStackPullRequest(nil),
	})
	gateway.EXPECT().CreatePullRequestStack(gomock.Any(), &github.CreatePullRequestStackInput{
		Owner:        "acme",
		Repo:         "repo",
		PullRequests: []int{2, 3},
	}).Return(nil)

	err := executeStackUpdate(t.Context(), newStackRepository(t, gateway), []forge.StackChange{
		{Change: &PR{Number: 1}, BaseBranch: "base"},
		{Change: &PR{Number: 2}, BaseChange: &PR{Number: 1}, BaseBranch: "base"},
		{Change: &PR{Number: 3}, BaseChange: &PR{Number: 2}, BaseBranch: "base"},
	})
	require.NoError(t, err)
}

func newStackRepository(t *testing.T, gateway githubGateway) *Repository {
	return &Repository{
		owner:   "acme",
		repo:    "repo",
		gateway: gateway,
		log:     silogtest.New(t),
	}
}

func newStackPullRequest(stack *github.PullRequestStack) *github.StackUpdatePullRequest {
	return &github.StackUpdatePullRequest{
		State:               github.PullRequestStateOpen,
		BaseRefName:         "base",
		Stack:               stack,
		HeadRepositoryOwner: "acme",
		HeadRepositoryName:  "repo",
	}
}

func newPullRequestStack(number int, members ...int) *github.PullRequestStack {
	stack := &github.PullRequestStack{Number: number}
	for _, member := range members {
		stack.Members = append(stack.Members, github.PullRequestStackMember{
			Number: member,
			State:  github.PullRequestStateOpen,
		})
	}
	return stack
}

func stackPullRequest(
	id github.ID,
	base string,
	stack *github.PullRequestStack,
) *github.StackUpdatePullRequest {
	pullRequest := newStackPullRequest(stack)
	pullRequest.ID = id
	pullRequest.BaseRefName = base
	return pullRequest
}

func expectPullRequests(
	t *testing.T,
	gateway *MockGithubGateway,
	pullRequests map[int]*github.StackUpdatePullRequest,
) {
	t.Helper()
	gateway.EXPECT().CheckPullRequestStacks(gomock.Any(), "acme", "repo").Return(nil)
	gateway.EXPECT().PullRequestsForStackUpdate(
		gomock.Any(),
		"acme",
		"repo",
		gomock.Any(),
	).DoAndReturn(func(
		_ context.Context,
		_, _ string,
		numbers []int,
	) ([]*github.StackUpdatePullRequest, error) {
		require.Len(t, numbers, len(pullRequests))
		result := make([]*github.StackUpdatePullRequest, len(numbers))
		for i, number := range numbers {
			var ok bool
			result[i], ok = pullRequests[number]
			require.True(t, ok, "unexpected pull request #%d", number)
		}
		return result, nil
	})
}

func executeStackUpdate(
	ctx context.Context,
	repository *Repository,
	changes []forge.StackChange,
) error {
	plan, err := repository.PlanStackUpdate(ctx, changes)
	if err != nil {
		return err
	}
	return plan.Execute(ctx)
}
