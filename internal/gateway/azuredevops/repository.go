package azuredevops

import (
	"context"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
)

// Repository identifies an Azure Repos Git repository.
type Repository struct {
	// ID is the repository UUID.
	// It is empty when Azure DevOps omits the ID from its response.
	ID string

	// Name is the repository's display name.
	// It is empty when Azure DevOps omits the name from its response.
	Name string
}

// Repository looks up a repository by name or ID within project.
// It returns [ErrNotFound] when Azure DevOps cannot find the repository.
func (g *Gateway) Repository(
	ctx context.Context,
	project string,
	repository string,
) (*Repository, error) {
	repo, err := g.gitClient.GetRepository(ctx, git.GetRepositoryArgs{
		Project:      &project,
		RepositoryId: &repository,
	})
	if err != nil {
		return nil, normalizeError(err)
	}

	result := &Repository{}
	if repo.Id != nil {
		result.ID = repo.Id.String()
	}
	if repo.Name != nil {
		result.Name = *repo.Name
	}
	return result, nil
}
