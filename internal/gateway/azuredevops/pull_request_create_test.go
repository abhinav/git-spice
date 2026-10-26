package azuredevops

import (
	"context"
	"testing"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestGateway_CreatePullRequest_fork(t *testing.T) {
	client := NewMockGitClient(gomock.NewController(t))
	client.EXPECT().CreatePullRequest(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, args git.CreatePullRequestArgs) (*git.GitPullRequest, error) {
			require.Equal(t, "project", *args.Project)
			require.Equal(t, "target-repository", *args.RepositoryId)
			request := args.GitPullRequestToCreate
			require.NotNil(t, request.ForkSource)
			require.NotNil(t, request.ForkSource.Repository)
			assert.Equal(t, "source-repository", *request.ForkSource.Repository.Name)
			return &git.GitPullRequest{PullRequestId: new(42)}, nil
		},
	)

	gateway := &Gateway{gitClient: client}
	pr, err := gateway.CreatePullRequest(t.Context(), &CreatePullRequestInput{
		Project:    "project",
		Repository: "target-repository",
		SourceRef:  "refs/heads/feature",
		TargetRef:  "refs/heads/main",
		ForkSource: &Repository{Name: "source-repository"},
	})

	require.NoError(t, err)
	assert.Equal(t, 42, pr.ID)
}
