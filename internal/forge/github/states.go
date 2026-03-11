package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
)

// ChangesStates retrieves the states of the given changes in bulk.
func (r *Repository) ChangesStates(ctx context.Context, ids []forge.ChangeID) ([]forge.ChangeState, error) {
	var q struct {
		Nodes []struct {
			PullRequest struct {
				State githubv4.PullRequestState `graphql:"state"`
			} `graphql:"... on PullRequest"`
		} `graphql:"nodes(ids: $ids)"`
	}

	gqlIDs := make([]githubv4.ID, len(ids))
	for i, id := range ids {
		pr := mustPR(id)
		var err error
		gqlIDs[i], err = r.graphQLID(ctx, pr)
		if err != nil {
			return nil, fmt.Errorf("resolve ID %v: %w", id, err)
		}
	}

	if err := r.client.Query(ctx, &q, map[string]any{"ids": gqlIDs}); err != nil {
		return nil, fmt.Errorf("retrieve change states: %w", err)
	}

	states := make([]forge.ChangeState, len(ids))
	for i, pr := range q.Nodes {
		states[i] = forgeChangeState(pr.PullRequest.State)
	}

	return states, nil
}

// ChangesDetails retrieves state, draft status, and review decision
// for the given changes in bulk.
func (r *Repository) ChangesDetails(ctx context.Context, ids []forge.ChangeID) ([]forge.ChangeDetails, error) {
	var q struct {
		Nodes []struct {
			PullRequest struct {
				State          githubv4.PullRequestState          `graphql:"state"`
				IsDraft        githubv4.Boolean                   `graphql:"isDraft"`
				ReviewDecision githubv4.PullRequestReviewDecision `graphql:"reviewDecision"`
				ReviewRequests struct {
					TotalCount githubv4.Int `graphql:"totalCount"`
				} `graphql:"reviewRequests"`
			} `graphql:"... on PullRequest"`
		} `graphql:"nodes(ids: $ids)"`
	}

	gqlIDs := make([]githubv4.ID, len(ids))
	for i, id := range ids {
		pr := mustPR(id)
		var err error
		gqlIDs[i], err = r.graphQLID(ctx, pr)
		if err != nil {
			return nil, fmt.Errorf("resolve ID %v: %w", id, err)
		}
	}

	if err := r.client.Query(ctx, &q, map[string]any{"ids": gqlIDs}); err != nil {
		return nil, fmt.Errorf("retrieve change details: %w", err)
	}

	details := make([]forge.ChangeDetails, len(ids))
	for i, node := range q.Nodes {
		pr := node.PullRequest
		details[i] = forge.ChangeDetails{
			State: forgeChangeState(pr.State),
			Draft: bool(pr.IsDraft),
			ReviewDecision: forgeReviewDecision(
				pr.ReviewDecision,
				int(pr.ReviewRequests.TotalCount),
			),
		}
	}

	return details, nil
}

// forgeReviewDecision maps a GitHub reviewDecision and pending review request
// count to a forge.ChangeReviewDecision.
//
// reviewDecision is only set when branch protection rules require a review.
// When reviewers have been requested but no such rule exists,
// reviewDecision is empty even though reviews are pending.
// In that case, a non-zero pendingReviewCount is used as a fallback
// to detect that reviews have been requested.
func forgeReviewDecision(
	d githubv4.PullRequestReviewDecision,
	pendingReviewCount int,
) forge.ChangeReviewDecision {
	switch d {
	case githubv4.PullRequestReviewDecisionApproved:
		return forge.ChangeReviewApproved
	case githubv4.PullRequestReviewDecisionChangesRequested:
		return forge.ChangeReviewChangesRequested
	case githubv4.PullRequestReviewDecisionReviewRequired:
		return forge.ChangeReviewRequired
	}

	// reviewDecision is null when no branch protection rule requires a review,
	// even if reviewers have been requested.
	// Fall back to the pending review request count.
	if pendingReviewCount > 0 {
		return forge.ChangeReviewRequired
	}

	return forge.ChangeReviewNoReview
}
