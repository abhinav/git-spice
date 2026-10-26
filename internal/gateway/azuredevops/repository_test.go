package azuredevops

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestGateway_Repository(t *testing.T) {
	wantID := uuid.MustParse("01234567-89ab-cdef-0123-456789abcdef")
	client := NewMockGitClient(gomock.NewController(t))
	client.EXPECT().GetRepository(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, args git.GetRepositoryArgs) (*git.GitRepository, error) {
			require.Equal(t, "project", *args.Project)
			require.Equal(t, "repository", *args.RepositoryId)
			return &git.GitRepository{
				Id:   &wantID,
				Name: new("repository"),
			}, nil
		},
	)

	gateway := &Gateway{gitClient: client}
	repository, err := gateway.Repository(
		t.Context(), "project", "repository",
	)

	require.NoError(t, err)
	assert.Equal(t, &Repository{
		ID:   wantID.String(),
		Name: "repository",
	}, repository)
}
