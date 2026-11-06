package azuredevops

import (
	"context"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
)

// Item is a file or directory in an Azure Repos Git repository.
type Item struct {
	// Path is the repository-rooted item path returned by Azure DevOps.
	Path string

	// Folder reports whether the item is a directory.
	Folder bool

	// Content contains file content when the request included content.
	// Directory items and list results normally leave it empty.
	Content string
}

// Items lists path and one level of its descendants.
// The returned order matches the Azure DevOps response order.
// It returns [ErrNotFound] when path does not exist.
func (g *Gateway) Items(
	ctx context.Context,
	project string,
	repository string,
	path string,
) ([]Item, error) {
	recursion := git.VersionControlRecursionTypeValues.OneLevel
	items, err := g.gitClient.GetItems(ctx, git.GetItemsArgs{
		Project:        &project,
		RepositoryId:   &repository,
		ScopePath:      &path,
		RecursionLevel: &recursion,
	})
	if err != nil {
		return nil, normalizeError(err)
	}

	result := make([]Item, 0, len(*items))
	for _, item := range *items {
		result = append(result, itemFromSDK(&item))
	}
	return result, nil
}

// Item returns one repository item and requests its file content.
// It returns [ErrNotFound] when path does not exist.
func (g *Gateway) Item(
	ctx context.Context,
	project string,
	repository string,
	path string,
) (*Item, error) {
	includeContent := true
	item, err := g.gitClient.GetItem(ctx, git.GetItemArgs{
		Project:        &project,
		RepositoryId:   &repository,
		Path:           &path,
		IncludeContent: &includeContent,
	})
	if err != nil {
		return nil, normalizeError(err)
	}
	result := itemFromSDK(item)
	return &result, nil
}

func itemFromSDK(item *git.GitItem) Item {
	result := Item{}
	if item.Path != nil {
		result.Path = *item.Path
	}
	if item.IsFolder != nil {
		result.Folder = *item.IsFolder
	}
	if item.Content != nil {
		result.Content = *item.Content
	}
	return result
}
