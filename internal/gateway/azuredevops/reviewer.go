package azuredevops

import (
	"context"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
)

// AddReviewer adds the materialized identity reviewerID to a pull request.
func (g *Gateway) AddReviewer(
	ctx context.Context,
	project string,
	repository string,
	pullRequest int,
	reviewerID string,
) error {
	vote := 0
	_, err := g.gitClient.CreatePullRequestReviewer(
		ctx,
		git.CreatePullRequestReviewerArgs{
			Project:       &project,
			RepositoryId:  &repository,
			PullRequestId: &pullRequest,
			ReviewerId:    &reviewerID,
			Reviewer: &git.IdentityRefWithVote{
				Id:   &reviewerID,
				Vote: &vote,
			},
		},
	)
	return normalizeError(err)
}

// AddReviewerByName asks Azure DevOps to resolve reviewer while adding it to
// a pull request.
func (g *Gateway) AddReviewerByName(
	ctx context.Context,
	project string,
	repository string,
	pullRequest int,
	reviewer string,
) error {
	vote := 0
	_, err := g.gitClient.CreateUnmaterializedPullRequestReviewer(
		ctx,
		git.CreateUnmaterializedPullRequestReviewerArgs{
			Project:       &project,
			RepositoryId:  &repository,
			PullRequestId: &pullRequest,
			Reviewer: &git.IdentityRefWithVote{
				UniqueName: &reviewer,
				Vote:       &vote,
			},
		},
	)
	return normalizeError(err)
}

// Reviewers returns one identifier for each reviewer
// attached to a pull request.
// Azure DevOps unique names are preferred over display names and identity IDs.
func (g *Gateway) Reviewers(
	ctx context.Context,
	project string,
	repository string,
	pullRequest int,
) ([]string, error) {
	reviewers, err := g.gitClient.GetPullRequestReviewers(
		ctx,
		git.GetPullRequestReviewersArgs{
			Project:       &project,
			RepositoryId:  &repository,
			PullRequestId: &pullRequest,
		},
	)
	if err != nil {
		return nil, normalizeError(err)
	}
	return reviewersFromSDK(reviewers), nil
}
