package github

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
	"go.uber.org/mock/gomock"
)

func TestRepository_PlanMergeRanges(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	stack := newPullRequestStack(42, 1, 2, 4)
	gateway.EXPECT().PullRequestsForMergeRange(
		gomock.Any(), "acme", "repo", []int{1, 2, 3, 4},
	).Return([]*github.MergeRangePullRequest{
		newMergeRangePullRequest("main", "a", "hash-1", stack),
		newMergeRangePullRequest("a", "b", "hash-2", stack),
		newMergeRangePullRequest("b", "c", "hash-3", nil),
		newMergeRangePullRequest("b", "d", "hash-4", stack),
	}, nil)

	pr1, pr2 := &PR{Number: 1}, &PR{Number: 2}
	pr3, pr4 := &PR{Number: 3}, &PR{Number: 4}
	plans, err := newStackRepository(t, gateway).PlanMergeRanges(
		t.Context(),
		[]forge.MergePlanChange{
			{Change: pr1},
			{Change: pr2, BaseChange: pr1},
			{Change: pr3, BaseChange: pr2},
			{Change: pr4, BaseChange: pr2},
		},
	)
	require.NoError(t, err)
	require.Len(t, plans, 2)
	assert.Equal(t, []int{1, 2, 4}, mergeRangePlanNumbers(plans[0]))
	assert.Equal(t, []int{3}, mergeRangePlanNumbers(plans[1]))
}

func TestRepository_PlanMergeRanges_ignoresMergedStackMembers(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	stack := &github.PullRequestStack{
		Number: 42,
		Members: []github.PullRequestStackMember{
			{Number: 9, State: github.PullRequestStateMerged},
			{Number: 1, State: github.PullRequestStateOpen},
			{Number: 2, State: github.PullRequestStateOpen},
		},
	}
	gateway.EXPECT().PullRequestsForMergeRange(
		gomock.Any(), "acme", "repo", []int{1, 2},
	).Return([]*github.MergeRangePullRequest{
		newMergeRangePullRequest("main", "a", "hash-1", stack),
		newMergeRangePullRequest("a", "b", "hash-2", stack),
	}, nil)

	pr1, pr2 := &PR{Number: 1}, &PR{Number: 2}
	plans, err := newStackRepository(t, gateway).PlanMergeRanges(
		t.Context(),
		[]forge.MergePlanChange{
			{Change: pr1},
			{Change: pr2, BaseChange: pr1},
		},
	)
	require.NoError(t, err)
	require.Len(t, plans, 1)
	assert.Equal(t, []int{1, 2}, mergeRangePlanNumbers(plans[0]))
}

func TestRepository_PlanMergeRanges_requiresLowestStackMember(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	stack := newPullRequestStack(42, 1, 2, 3)
	gateway.EXPECT().PullRequestsForMergeRange(
		gomock.Any(), "acme", "repo", []int{2, 3},
	).Return([]*github.MergeRangePullRequest{
		newMergeRangePullRequest("a", "b", "hash-2", stack),
		newMergeRangePullRequest("b", "c", "hash-3", stack),
	}, nil)

	pr2, pr3 := &PR{Number: 2}, &PR{Number: 3}
	plans, err := newStackRepository(t, gateway).PlanMergeRanges(
		t.Context(),
		[]forge.MergePlanChange{
			{Change: pr2},
			{Change: pr3, BaseChange: pr2},
		},
	)
	require.NoError(t, err)
	assert.Empty(t, plans)
}

func TestRepository_PlanMergeRanges_rechecksNativeMembership(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	gateway := NewMockGithubGateway(mockCtrl)
	repo := newStackRepository(t, gateway)
	stack := newPullRequestStack(42, 1, 2)
	gateway.EXPECT().
		PullRequestsForMergeRange(gomock.Any(), "acme", "repo", []int{1, 2}).
		Return([]*github.MergeRangePullRequest{
			newMergeRangePullRequest("main", "a", "hash-1", stack),
			newMergeRangePullRequest("a", "b", "hash-2", stack),
		}, nil)
	plans, err := repo.PlanMergeRanges(t.Context(), []forge.MergePlanChange{
		{Change: &PR{Number: 1}},
		{Change: &PR{Number: 2}, BaseChange: &PR{Number: 1}},
	})
	require.NoError(t, err)
	require.Len(t, plans, 1)

	// Another native member was inserted below the plan.
	// Matching prepared commits must not authorize merging that PR.
	stack = newPullRequestStack(42, 3, 1, 2)
	gateway.EXPECT().
		PullRequestsForMergeRange(gomock.Any(), "acme", "repo", []int{1, 2}).
		Return([]*github.MergeRangePullRequest{
			newMergeRangePullRequest("lower", "a", "hash-1", stack),
			newMergeRangePullRequest("a", "b", "hash-2", stack),
		}, nil)
	operation, err := plans[0].Merge(t.Context(), forge.MergeRangeRequest{
		Heads: []forge.ChangeHead{
			{Change: &PR{Number: 1}, HeadHash: "hash-1"},
			{Change: &PR{Number: 2}, HeadHash: "hash-2"},
		},
	})
	assert.Nil(t, operation)
	assert.ErrorContains(t, err, "must begin with the lowest open member")
}

func TestRepository_MergeRangeImmediate(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	stack := newPullRequestStack(42, 1, 2, 3)
	gateway.EXPECT().PullRequestsForMergeRange(
		gomock.Any(), "acme", "repo", []int{1, 2},
	).Return([]*github.MergeRangePullRequest{
		newMergeRangePullRequest("main", "bottom", "hash-1", stack),
		newMergeRangePullRequest("bottom", "top", "hash-2", stack),
	}, nil)
	gateway.EXPECT().MergePullRequestAsync(gomock.Any(), &github.MergePullRequestAsyncInput{
		Owner:             "acme",
		Repo:              "repo",
		PullRequestNumber: 2,
		ExpectedHeadSHA:   "hash-2",
		Method:            github.MergeMethodSquash,
	}).Return(&github.AsyncMergeResult{
		Status: github.AsyncMergeStatusMerged,
	}, nil)

	operation, err := newMergeRangePlan(t, gateway, 1, 2).Merge(
		t.Context(),
		forge.MergeRangeRequest{
			Method: forge.MergeMethodSquash,
			Heads: []forge.ChangeHead{
				{Change: &PR{Number: 2}, HeadHash: "hash-2"},
				{Change: &PR{Number: 1}, HeadHash: "hash-1"},
			},
		},
	)
	require.NoError(t, err)
	assert.Nil(t, operation)
}

func TestRepository_MergeRangePendingOperation(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	gateway.EXPECT().PullRequestsForMergeRange(
		gomock.Any(), "acme", "repo", []int{1},
	).Return([]*github.MergeRangePullRequest{
		newMergeRangePullRequest("main", "feature", "hash-1", nil),
	}, nil)
	gateway.EXPECT().MergePullRequestAsync(gomock.Any(), &github.MergePullRequestAsyncInput{
		Owner:             "acme",
		Repo:              "repo",
		PullRequestNumber: 1,
		ExpectedHeadSHA:   "hash-1",
		Method:            github.MergeMethodUnknown,
	}).Return(
		&github.AsyncMergeResult{
			Status:      github.AsyncMergeStatusPending,
			OperationID: "operation-1",
		},
		nil,
	)
	gateway.EXPECT().AsyncMergeResult(
		gomock.Any(), "acme", "repo", 1, "operation-1",
	).Return(&github.AsyncMergeResult{
		Status: github.AsyncMergeStatusEnqueued,
	}, nil)

	operation, err := newMergeRangePlan(t, gateway, 1).Merge(
		t.Context(),
		forge.MergeRangeRequest{Heads: []forge.ChangeHead{
			{Change: &PR{Number: 1}, HeadHash: "hash-1"},
		}},
	)
	require.NoError(t, err)
	require.NotNil(t, operation)

	status, err := operation.Status(t.Context())
	require.NoError(t, err)
	assert.Equal(t, forge.MergeOperationAccepted, status)
}

func TestRepository_MergeRangeRejectsRemoteHeadChange(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	gateway.EXPECT().PullRequestsForMergeRange(
		gomock.Any(), "acme", "repo", []int{1},
	).Return([]*github.MergeRangePullRequest{
		newMergeRangePullRequest("main", "feature", "1234567bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", nil),
	}, nil)

	operation, err := newMergeRangePlan(t, gateway, 1).Merge(
		t.Context(),
		forge.MergeRangeRequest{Heads: []forge.ChangeHead{
			{Change: &PR{Number: 1}, HeadHash: "1234567aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		}},
	)
	assert.Nil(t, operation)
	assert.ErrorContains(t, err, "expected head")
}

func TestGithubMergeRangePlan_Merge_checksPlannedPath(t *testing.T) {
	tests := []struct {
		name       string
		bottomHash string
		topBase    string
		wantError  string
	}{
		{
			name:       "ChangedLowerHead",
			bottomHash: "new-hash-1",
			topBase:    "bottom",
			wantError:  `merge PR #1: expected head "hash-1", got "new-hash-1"`,
		},
		{
			name:       "MisalignedBase",
			bottomHash: "hash-1",
			topBase:    "other",
			wantError:  `merge PR #2: expected base "bottom", got "other"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := NewMockGithubGateway(gomock.NewController(t))
			stack := newPullRequestStack(42, 1, 2)
			gateway.EXPECT().PullRequestsForMergeRange(
				gomock.Any(), "acme", "repo", []int{1, 2},
			).Return([]*github.MergeRangePullRequest{
				newMergeRangePullRequest("main", "bottom", tt.bottomHash, stack),
				newMergeRangePullRequest(tt.topBase, "top", "hash-2", stack),
			}, nil)

			operation, err := newMergeRangePlan(t, gateway, 1, 2).Merge(
				t.Context(),
				forge.MergeRangeRequest{Heads: []forge.ChangeHead{
					{Change: &PR{Number: 2}, HeadHash: "hash-2"},
					{Change: &PR{Number: 1}, HeadHash: "hash-1"},
				}},
			)
			assert.Nil(t, operation)
			assert.EqualError(t, err, tt.wantError)
		})
	}
}

func TestRepository_MergeRangeRequiresNativeStack(t *testing.T) {
	tests := []struct {
		name        string
		bottomStack *github.PullRequestStack
		topStack    *github.PullRequestStack
		wantError   string
	}{
		{
			name:        "UnstackedRange",
			bottomStack: nil,
			topStack:    nil,
			wantError:   "merge range pull request #1 is not in a GitHub native stack",
		},
		{
			name:        "UnstackedBottom",
			bottomStack: nil,
			topStack:    newPullRequestStack(42, 2, 3),
			wantError:   "merge range pull request #1 is not in a GitHub native stack",
		},
		{
			name:        "UnstackedTop",
			bottomStack: newPullRequestStack(42, 1, 2),
			topStack:    nil,
			wantError:   "merge range pull request #2 is not in a GitHub native stack",
		},
		{
			name:        "DifferentStacks",
			bottomStack: newPullRequestStack(42, 1),
			topStack:    newPullRequestStack(43, 2),
			wantError:   "merge range belongs to different GitHub native stacks",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := NewMockGithubGateway(gomock.NewController(t))
			gateway.EXPECT().PullRequestsForMergeRange(
				gomock.Any(), "acme", "repo", []int{1, 2},
			).Return([]*github.MergeRangePullRequest{
				newMergeRangePullRequest("main", "bottom", "hash-1", tt.bottomStack),
				newMergeRangePullRequest("bottom", "top", "hash-2", tt.topStack),
			}, nil)

			operation, err := newMergeRangePlan(t, gateway, 1, 2).Merge(
				t.Context(),
				forge.MergeRangeRequest{Heads: []forge.ChangeHead{
					{Change: &PR{Number: 1}, HeadHash: "hash-1"},
					{Change: &PR{Number: 2}, HeadHash: "hash-2"},
				}},
			)
			assert.Nil(t, operation)
			assert.EqualError(t, err, tt.wantError)
		})
	}
}

func TestGithubMergeRangePlan_Merge_requiresLowestStackMember(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	gateway.EXPECT().PullRequestsForMergeRange(
		gomock.Any(), "acme", "repo", []int{2},
	).Return([]*github.MergeRangePullRequest{
		newMergeRangePullRequest("bottom", "top", "hash-2", newPullRequestStack(42, 1, 2)),
	}, nil)

	operation, err := newMergeRangePlan(t, gateway, 2).Merge(
		t.Context(),
		forge.MergeRangeRequest{Heads: []forge.ChangeHead{
			{Change: &PR{Number: 2}, HeadHash: "hash-2"},
		}},
	)
	assert.Nil(t, operation)
	assert.ErrorContains(t, err, "must begin with the lowest open member")
}

func TestRepository_MergeRangeUnsupported(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	gateway.EXPECT().PullRequestsForMergeRange(
		gomock.Any(), "acme", "repo", []int{1},
	).Return([]*github.MergeRangePullRequest{
		newMergeRangePullRequest("main", "feature", "hash-1", nil),
	}, nil)
	gateway.EXPECT().MergePullRequestAsync(gomock.Any(), gomock.Any()).Return(
		nil,
		github.ErrNotFound,
	)

	operation, err := newMergeRangePlan(t, gateway, 1).Merge(
		t.Context(),
		forge.MergeRangeRequest{Heads: []forge.ChangeHead{
			{Change: &PR{Number: 1}, HeadHash: "hash-1"},
		}},
	)
	assert.Nil(t, operation)
	require.Error(t, err)
	assert.ErrorIs(t, err, forge.ErrUnsupported)
	assert.ErrorIs(t, err, github.ErrNotFound)
}

func newMergeRangePlan(
	t *testing.T,
	gateway *MockGithubGateway,
	numbers ...int,
) *githubMergeRangePlan {
	changes := make([]*PR, len(numbers))
	for i, number := range numbers {
		changes[i] = &PR{Number: number}
	}
	return &githubMergeRangePlan{
		Repo: newStackRepository(t, gateway),
		PRs:  changes,
	}
}

func mergeRangePlanNumbers(plan forge.MergeRangePlan) []int {
	return slices.Collect(func(yield func(int) bool) {
		for _, change := range plan.Changes() {
			if !yield(mustPR(change).Number) {
				return
			}
		}
	})
}

func newMergeRangePullRequest(
	base string,
	head string,
	headHash string,
	stack *github.PullRequestStack,
) *github.MergeRangePullRequest {
	return &github.MergeRangePullRequest{
		State:               github.PullRequestStateOpen,
		BaseRefName:         base,
		HeadRefName:         head,
		HeadRefOID:          headHash,
		HeadRepositoryOwner: "acme",
		HeadRepositoryName:  "repo",
		Stack:               stack,
	}
}
