package azuredevops

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/core"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/identity"
)

func (r *Repository) addLabelsToPullRequest(
	ctx context.Context,
	prID int,
	labels []string,
) error {
	for _, label := range labels {
		_, err := r.client.gitClient.CreatePullRequestLabel(
			ctx,
			git.CreatePullRequestLabelArgs{
				Project:       new(r.project()),
				RepositoryId:  new(r.repositoryID()),
				PullRequestId: &prID,
				Label: &core.WebApiCreateTagRequestData{
					Name: &label,
				},
			},
		)
		if err != nil {
			return fmt.Errorf("add label %q: %w", label, err)
		}
	}
	return nil
}

func (r *Repository) addReviewersToPullRequest(
	ctx context.Context,
	prID int,
	reviewers []string,
) error {
	for _, reviewer := range reviewers {
		if err := r.addReviewerToPullRequest(ctx, prID, reviewer); err != nil {
			return fmt.Errorf("add reviewer %q: %w", reviewer, err)
		}
	}
	return nil
}

func (r *Repository) addReviewerToPullRequest(
	ctx context.Context,
	prID int,
	reviewer string,
) error {
	vote := 0
	if reviewerID := r.reviewerID(ctx, reviewer); reviewerID != "" {
		_, err := r.client.gitClient.CreatePullRequestReviewer(
			ctx,
			git.CreatePullRequestReviewerArgs{
				Project:       new(r.project()),
				RepositoryId:  new(r.repositoryID()),
				PullRequestId: &prID,
				ReviewerId:    &reviewerID,
				Reviewer: &git.IdentityRefWithVote{
					Id:   &reviewerID,
					Vote: &vote,
				},
			},
		)
		return err
	}

	_, err := r.client.gitClient.CreateUnmaterializedPullRequestReviewer(
		ctx,
		git.CreateUnmaterializedPullRequestReviewerArgs{
			Project:       new(r.project()),
			RepositoryId:  new(r.repositoryID()),
			PullRequestId: &prID,
			Reviewer: &git.IdentityRefWithVote{
				UniqueName: &reviewer,
				Vote:       &vote,
			},
		},
	)
	return err
}

func (r *Repository) reviewerID(ctx context.Context, reviewer string) string {
	if id, ok := r.reviewerIDs[reviewer]; ok {
		return id
	}
	if _, err := uuid.Parse(reviewer); err == nil {
		return reviewer
	}

	id, err := r.resolveReviewerID(ctx, reviewer)
	if err != nil {
		return ""
	}
	return id
}

func (r *Repository) resolveReviewerID(ctx context.Context, reviewer string) (string, error) {
	if r.client.identityClient == nil {
		return "", errors.New("identity client unavailable")
	}

	for _, searchFilter := range []string{"MailAddress", "General"} {
		identities, err := r.client.identityClient.ReadIdentities(
			ctx,
			identity.ReadIdentitiesArgs{
				SearchFilter: &searchFilter,
				FilterValue:  &reviewer,
			},
		)
		if err != nil {
			return "", err
		}

		for _, id := range *identities {
			if id.Id != nil {
				return id.Id.String(), nil
			}
		}
	}
	return "", fmt.Errorf("reviewer %q not found", reviewer)
}

func (r *Repository) prLabels(
	ctx context.Context,
	prID int,
	labels *[]core.WebApiTagDefinition,
) ([]string, error) {
	if labels != nil {
		return labelsFromPR(labels), nil
	}

	labels, err := r.client.gitClient.GetPullRequestLabels(
		ctx,
		git.GetPullRequestLabelsArgs{
			Project:       new(r.project()),
			RepositoryId:  new(r.repositoryID()),
			PullRequestId: &prID,
		},
	)
	if err != nil {
		return nil, err
	}
	return labelsFromPR(labels), nil
}

func labelsFromPR(labels *[]core.WebApiTagDefinition) []string {
	if labels == nil || len(*labels) == 0 {
		return nil
	}

	result := make([]string, 0, len(*labels))
	for _, label := range *labels {
		if label.Name == nil {
			continue
		}
		result = append(result, *label.Name)
	}
	return result
}

func (r *Repository) prReviewers(
	ctx context.Context,
	prID int,
	reviewers *[]git.IdentityRefWithVote,
) ([]string, error) {
	if reviewers != nil {
		return reviewersFromPR(reviewers), nil
	}

	reviewers, err := r.client.gitClient.GetPullRequestReviewers(
		ctx,
		git.GetPullRequestReviewersArgs{
			Project:       new(r.project()),
			RepositoryId:  new(r.repositoryID()),
			PullRequestId: &prID,
		},
	)
	if err != nil {
		return nil, err
	}
	return reviewersFromPR(reviewers), nil
}

func reviewersFromPR(reviewers *[]git.IdentityRefWithVote) []string {
	if reviewers == nil || len(*reviewers) == 0 {
		return nil
	}

	result := make([]string, 0, len(*reviewers))
	for _, reviewer := range *reviewers {
		name := reviewerName(&reviewer)
		if name == "" {
			continue
		}
		result = append(result, name)
	}
	return result
}

func reviewerName(reviewer *git.IdentityRefWithVote) string {
	switch {
	case reviewer.UniqueName != nil:
		return *reviewer.UniqueName
	case reviewer.DisplayName != nil:
		return *reviewer.DisplayName
	case reviewer.Id != nil:
		return *reviewer.Id
	default:
		return ""
	}
}
