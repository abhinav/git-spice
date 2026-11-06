package azuredevops

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

func (r *Repository) addLabelsToPullRequest(
	ctx context.Context,
	prID int,
	labels []string,
) error {
	for _, label := range labels {
		err := r.gateway.AddLabel(
			ctx, r.project(), r.repositoryID(), prID, label,
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
	if reviewerID := r.reviewerID(ctx, reviewer); reviewerID != "" {
		return r.gateway.AddReviewer(
			ctx, r.project(), r.repositoryID(), prID, reviewerID,
		)
	}

	return r.gateway.AddReviewerByName(
		ctx, r.project(), r.repositoryID(), prID, reviewer,
	)
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
	return r.gateway.ReviewerID(ctx, reviewer)
}

func (r *Repository) prLabels(
	ctx context.Context,
	prID int,
	labels *[]string,
) ([]string, error) {
	if labels != nil {
		return *labels, nil
	}

	result, err := r.gateway.Labels(ctx, r.project(), r.repositoryID(), prID)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Repository) prReviewers(
	ctx context.Context,
	prID int,
	reviewers *[]string,
) ([]string, error) {
	if reviewers != nil {
		return *reviewers, nil
	}

	result, err := r.gateway.Reviewers(ctx, r.project(), r.repositoryID(), prID)
	if err != nil {
		return nil, err
	}
	return result, nil
}
