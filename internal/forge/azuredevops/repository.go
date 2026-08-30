package azuredevops

import (
	"context"
	"fmt"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/silog"
)

// Repository is an Azure DevOps repository.
type Repository struct {
	forge  *Forge
	repoID *RepositoryID
	log    *silog.Logger
	client *azureDevOpsClient

	// Cached repository info from API.
	repoInfo *git.GitRepository

	// reviewerIDs maps reviewer names to Azure DevOps identity IDs.
	reviewerIDs map[string]string
}

var _ forge.Repository = (*Repository)(nil)

func newRepository(
	ctx context.Context,
	f *Forge,
	repoID *RepositoryID,
	log *silog.Logger,
	client *azureDevOpsClient,
) (*Repository, error) {
	log = log.With(
		"org", repoID.organization,
		"project", repoID.project,
		"repo", repoID.repository,
	)

	// Get the repository info to ensure it exists and get the UUID.
	repoInfo, err := client.gitClient.GetRepository(ctx, git.GetRepositoryArgs{
		Project:      &repoID.project,
		RepositoryId: &repoID.repository,
	})
	if err != nil {
		return nil, fmt.Errorf("get repository: %w", err)
	}

	return &Repository{
		forge:    f,
		repoID:   repoID,
		log:      log,
		client:   client,
		repoInfo: repoInfo,
	}, nil
}

// Forge returns the forge this repository belongs to.
func (r *Repository) Forge() forge.Forge { return r.forge }

// repositoryID returns the repository ID as a string for API calls.
func (r *Repository) repositoryID() string {
	if r.repoInfo != nil && r.repoInfo.Id != nil {
		return r.repoInfo.Id.String()
	}
	return r.repoID.repository
}

// project returns the project name for API calls.
func (r *Repository) project() string {
	return r.repoID.project
}

func (r *Repository) getRepository(
	ctx context.Context,
	id forge.RepositoryID,
) (*git.GitRepository, error) {
	rid := mustRepositoryID(id)
	if rid.organization != r.repoID.organization {
		return nil, fmt.Errorf(
			"repository %q belongs to organization %q, not %q",
			rid.repository, rid.organization, r.repoID.organization,
		)
	}

	repo, err := r.client.gitClient.GetRepository(ctx, git.GetRepositoryArgs{
		Project:      &rid.project,
		RepositoryId: &rid.repository,
	})
	if err != nil {
		return nil, fmt.Errorf("get repository: %w", err)
	}
	return repo, nil
}
