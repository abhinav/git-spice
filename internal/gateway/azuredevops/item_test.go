package azuredevops

import (
	"context"
	"net/http"
	"testing"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestGateway_Items(t *testing.T) {
	client := NewMockGitClient(gomock.NewController(t))
	client.EXPECT().GetItems(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, args git.GetItemsArgs) (*[]git.GitItem, error) {
			require.Equal(t, "project", *args.Project)
			require.Equal(t, "repository", *args.RepositoryId)
			require.Equal(t, "/templates", *args.ScopePath)
			require.Equal(
				t,
				git.VersionControlRecursionTypeValues.OneLevel,
				*args.RecursionLevel,
			)
			return &[]git.GitItem{
				{Path: new("/templates"), IsFolder: new(true)},
				{Path: new("/templates/pull-request.md")},
			}, nil
		},
	)

	gateway := &Gateway{gitClient: client}
	items, err := gateway.Items(
		t.Context(), "project", "repository", "/templates",
	)

	require.NoError(t, err)
	assert.Equal(t, []Item{
		{Path: "/templates", Folder: true},
		{Path: "/templates/pull-request.md"},
	}, items)
}

func TestGateway_Item_requestsContent(t *testing.T) {
	client := NewMockGitClient(gomock.NewController(t))
	client.EXPECT().GetItem(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, args git.GetItemArgs) (*git.GitItem, error) {
			require.True(t, *args.IncludeContent)
			return &git.GitItem{
				Path:    new("/pull-request.md"),
				Content: new("Template body\n"),
			}, nil
		},
	)

	gateway := &Gateway{gitClient: client}
	item, err := gateway.Item(
		t.Context(), "project", "repository", "/pull-request.md",
	)

	require.NoError(t, err)
	assert.Equal(t, &Item{
		Path:    "/pull-request.md",
		Content: "Template body\n",
	}, item)
}

func TestGateway_Item_notFound(t *testing.T) {
	statusCode := http.StatusNotFound
	client := NewMockGitClient(gomock.NewController(t))
	client.EXPECT().GetItem(gomock.Any(), gomock.Any()).Return(
		nil,
		azuredevops.WrappedError{StatusCode: &statusCode},
	)

	gateway := &Gateway{gitClient: client}
	_, err := gateway.Item(
		t.Context(), "project", "repository", "/missing.md",
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotFound)
}
