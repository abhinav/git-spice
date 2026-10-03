package github

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/gateway/github"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.uber.org/mock/gomock"
)

func TestGitHubStackTransition_Execute_stopsAfterPrerequisiteFailure(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	gateway.EXPECT().UnstackPullRequestStack(
		gomock.Any(),
		&github.UnstackPullRequestStackInput{
			Owner:       "acme",
			Repo:        "repo",
			StackNumber: 42,
		},
	).Return(nil, github.ErrForbidden)
	transition := githubStackTransition{
		unstackNumber: 42,
		paths: []githubStackPathTransition{{
			stackUpdate: &githubStackMembershipUpdate{
				pullRequests: []int{1, 2},
			},
		}},
	}
	repository := &Repository{
		owner:   "acme",
		repo:    "repo",
		gateway: gateway,
		log:     silogtest.New(t),
	}

	err := transition.execute(t.Context(), repository)
	require.ErrorIs(t, err, github.ErrForbidden)
}

func TestGitHubStackTransition_Execute_continuesIndependentPath(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	gateway.EXPECT().UpdatePullRequest(gomock.Any(), &github.UpdatePullRequestInput{
		PullRequestID: "PR_2",
		BaseRefName:   new("a"),
	}).Return(github.ErrForbidden)
	gateway.EXPECT().CreatePullRequestStack(gomock.Any(), &github.CreatePullRequestStackInput{
		Owner:        "acme",
		Repo:         "repo",
		PullRequests: []int{10, 11},
	}).Return(nil)
	transition := githubStackTransition{
		paths: []githubStackPathTransition{
			{
				baseUpdates: []githubPullRequestBaseUpdate{{
					pullRequestNumber: 2,
					pullRequestID:     "PR_2",
					baseBranch:        "a",
				}},
				stackUpdate: &githubStackMembershipUpdate{
					pullRequests: []int{1, 2},
				},
			},
			{
				stackUpdate: &githubStackMembershipUpdate{
					pullRequests: []int{10, 11},
				},
			},
		},
	}
	repository := &Repository{
		owner:   "acme",
		repo:    "repo",
		gateway: gateway,
		log:     silogtest.New(t),
	}

	err := transition.execute(t.Context(), repository)
	require.ErrorIs(t, err, github.ErrForbidden)
}
