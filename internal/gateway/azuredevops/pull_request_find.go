package azuredevops

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
)

// FindPullRequestsInput describes a pull request search.
type FindPullRequestsInput struct {
	// Project identifies the Azure DevOps project to search.
	Project string

	// Repository identifies the target repository by name or UUID.
	Repository string

	// SourceRef limits matches to this full Git source ref.
	SourceRef string

	// SourceRepository limits matches to this source repository UUID.
	// An empty value searches without a source repository constraint.
	SourceRepository string

	// Status limits matches to one lifecycle state.
	// PullRequestStatusUnknown searches all states.
	Status PullRequestStatus

	// Limit is the maximum number of pull requests returned by Azure DevOps.
	Limit int
}

// FindPullRequests returns pull requests matching the supplied criteria.
func (g *Gateway) FindPullRequests(
	ctx context.Context,
	in *FindPullRequestsInput,
) ([]*PullRequest, error) {
	status := pullRequestStatusToSDK(in.Status)
	criteria := &git.GitPullRequestSearchCriteria{
		SourceRefName: &in.SourceRef,
		Status:        &status,
	}
	if in.SourceRepository != "" {
		id, err := uuid.Parse(in.SourceRepository)
		if err != nil {
			return nil, fmt.Errorf("parse source repository ID: %w", err)
		}
		criteria.SourceRepositoryId = &id
	}

	prs, err := g.gitClient.GetPullRequests(ctx, git.GetPullRequestsArgs{
		Project:        &in.Project,
		RepositoryId:   &in.Repository,
		SearchCriteria: criteria,
		Top:            &in.Limit,
	})
	if err != nil {
		return nil, normalizeError(err)
	}
	if prs == nil {
		return nil, nil
	}

	result := make([]*PullRequest, 0, len(*prs))
	for i := range *prs {
		result = append(result, pullRequestFromSDK(&(*prs)[i]))
	}
	return result, nil
}
