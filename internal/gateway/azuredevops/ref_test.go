package azuredevops

import (
	"context"
	"testing"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestGateway_RefExists_requiresExactRef(t *testing.T) {
	client := NewMockGitClient(gomock.NewController(t))
	client.EXPECT().GetRefs(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, args git.GetRefsArgs) (*git.GetRefsResponseValue, error) {
			require.Equal(t, "heads/feature", *args.Filter)
			return &git.GetRefsResponseValue{Value: []git.GitRef{
				{Name: new("refs/heads/feature-extra")},
				{Name: new("refs/heads/feature")},
			}}, nil
		},
	)

	gateway := &Gateway{gitClient: client}
	exists, err := gateway.RefExists(
		t.Context(), "project", "repository", "heads/feature",
	)

	require.NoError(t, err)
	assert.True(t, exists)
}
