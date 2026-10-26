package azuredevops

import (
	"context"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
)

// CreatePullRequestInput describes a pull request to create.
type CreatePullRequestInput struct {
	// Project identifies the Azure DevOps project containing Repository.
	Project string

	// Repository identifies the target repository by name or UUID.
	Repository string

	// Title is the pull request title.
	Title string

	// Description is the pull request body.
	Description string

	// SourceRef is the full Git ref containing the proposed changes.
	SourceRef string

	// TargetRef is the full Git ref that will receive the changes.
	TargetRef string

	// Draft controls whether Azure DevOps creates a draft pull request.
	Draft bool

	// ForkSource identifies the source repository for a fork pull request.
	// A nil value creates the pull request from Repository itself.
	ForkSource *Repository
}

// CreatePullRequest creates a pull request and returns its normalized state.
func (g *Gateway) CreatePullRequest(
	ctx context.Context,
	in *CreatePullRequestInput,
) (*PullRequest, error) {
	request := &git.GitPullRequest{
		Title:         &in.Title,
		Description:   &in.Description,
		SourceRefName: &in.SourceRef,
		TargetRefName: &in.TargetRef,
		IsDraft:       &in.Draft,
	}
	if in.ForkSource != nil {
		name := in.ForkSource.Name
		request.ForkSource = &git.GitForkRef{
			Repository: &git.GitRepository{Name: &name},
		}
	}

	pr, err := g.gitClient.CreatePullRequest(ctx, git.CreatePullRequestArgs{
		Project:                &in.Project,
		RepositoryId:           &in.Repository,
		GitPullRequestToCreate: request,
	})
	if err != nil {
		return nil, normalizeError(err)
	}
	return pullRequestFromSDK(pr), nil
}
