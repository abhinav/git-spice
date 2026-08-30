package azuredevops

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/azuredevops"
	"go.uber.org/mock/gomock"
)

func TestRepository_SubmitChange_pushRepository(t *testing.T) {
	var gotInput *azuredevops.CreatePullRequestInput
	gateway := NewMockAzureDevOpsGateway(gomock.NewController(t))
	gateway.EXPECT().Repository(gomock.Any(), "forkproject", "myfork").Return(
		&azuredevops.Repository{Name: "myfork"}, nil,
	)
	gateway.EXPECT().CreatePullRequest(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, input *azuredevops.CreatePullRequestInput) (*azuredevops.PullRequest, error) {
			gotInput = input
			return &azuredevops.PullRequest{ID: 42}, nil
		},
	)
	repo := newTestRepository(gateway)
	pushRepository := &RepositoryID{
		organization: "myorg",
		project:      "forkproject",
		repository:   "myfork",
	}

	_, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject:        "Test PR",
		Base:           "main",
		Head:           "feature",
		PushRepository: pushRepository,
	})
	require.NoError(t, err)

	require.NotNil(t, gotInput.ForkSource)
	assert.Equal(t, "myfork", gotInput.ForkSource.Name)
}

func TestRepository_FindChangesByBranch_pushRepository(t *testing.T) {
	var gotInput *azuredevops.FindPullRequestsInput
	gateway := NewMockAzureDevOpsGateway(gomock.NewController(t))
	gateway.EXPECT().FindPullRequests(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, input *azuredevops.FindPullRequestsInput) ([]*azuredevops.PullRequest, error) {
			gotInput = input
			return nil, nil
		},
	)
	repo := newTestRepository(gateway)
	pushRepositoryID := "22222222-2222-2222-2222-222222222222"
	pushRepository := &RepositoryID{
		organization: "myorg",
		project:      "forkproject",
		repository:   "myfork",
	}
	gateway.EXPECT().Repository(gomock.Any(), "forkproject", "myfork").Return(
		&azuredevops.Repository{ID: pushRepositoryID}, nil,
	)

	_, err := repo.FindChangesByBranch(t.Context(), "feature", forge.FindChangesOptions{
		PushRepository: pushRepository,
	})
	require.NoError(t, err)

	assert.Equal(t, pushRepositoryID, gotInput.SourceRepository)
}
