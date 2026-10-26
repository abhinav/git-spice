package azuredevops

import (
	"context"
	"testing"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestGateway_UpdatePullRequest_defaultCompletionStrategy(t *testing.T) {
	client := NewMockGitClient(gomock.NewController(t))
	client.EXPECT().UpdatePullRequest(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, args git.UpdatePullRequestArgs) (*git.GitPullRequest, error) {
			request := args.GitPullRequestToUpdate
			require.NotNil(t, request.CompletionOptions)
			assert.Nil(t, request.CompletionOptions.MergeStrategy)
			return &git.GitPullRequest{}, nil
		},
	)

	gateway := &Gateway{gitClient: client}
	err := gateway.UpdatePullRequest(t.Context(), &UpdatePullRequestInput{
		Project:    "project",
		Repository: "repository",
		ID:         42,
		Completion: &CompletionOptions{},
	})

	require.NoError(t, err)
}
