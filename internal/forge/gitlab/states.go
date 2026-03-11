package gitlab

import (
	"context"
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.abhg.dev/gs/internal/forge"
)

// ChangesStates retrieves the states of the given changes in bulk.
func (r *Repository) ChangesStates(ctx context.Context, ids []forge.ChangeID) ([]forge.ChangeState, error) {
	mrIDs := make([]int64, len(ids))
	for i, id := range ids {
		mrIDs[i] = mustMR(id).Number
	}

	allStates := "all"
	mergeRequests, _, err := r.client.MergeRequests.ListProjectMergeRequests(
		r.repoID, &gitlab.ListProjectMergeRequestsOptions{
			IIDs:  &mrIDs,
			State: &allStates,
		},
		gitlab.WithContext(ctx),
	)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	// Create a map of MR IDs to MRs.
	mrMap := make(map[int64]*gitlab.BasicMergeRequest)
	for _, mr := range mergeRequests {
		mrMap[mr.IID] = mr
	}

	states := make([]forge.ChangeState, len(mrIDs))
	for i, id := range mrIDs {
		mr, ok := mrMap[id]
		if !ok {
			// MR not returned; leave zero-value state so callers can detect it.
			continue
		}
		switch mr.State {
		case "opened":
			states[i] = forge.ChangeOpen
		case "merged":
			states[i] = forge.ChangeMerged
		case "closed":
			states[i] = forge.ChangeClosed
		default:
			states[i] = forge.ChangeOpen // default to open for unknown states
		}
	}

	return states, nil
}

// ChangesDetails retrieves state, draft status, and review decision
// for the given changes in bulk.
func (r *Repository) ChangesDetails(ctx context.Context, ids []forge.ChangeID) ([]forge.ChangeDetails, error) {
	mrIDs := make([]int64, len(ids))
	for i, id := range ids {
		mrIDs[i] = mustMR(id).Number
	}

	allStates := "all"
	mergeRequests, _, err := r.client.MergeRequests.ListProjectMergeRequests(
		r.repoID, &gitlab.ListProjectMergeRequestsOptions{
			IIDs:  &mrIDs,
			State: &allStates,
		},
		gitlab.WithContext(ctx),
	)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	mrMap := make(map[int64]*gitlab.BasicMergeRequest)
	for _, mr := range mergeRequests {
		mrMap[mr.IID] = mr
	}

	details := make([]forge.ChangeDetails, len(mrIDs))
	for i, id := range mrIDs {
		mr, ok := mrMap[id]
		if !ok {
			// MR not found; return zero-value details.
			continue
		}
		details[i] = forge.ChangeDetails{
			State: forgeChangeState(mr.State),
			Draft: mr.Draft,
			ReviewDecision: gitlabReviewDecision(
				mr.Reviewers,
				mr.DetailedMergeStatus,
			),
		}
	}

	return details, nil
}

// gitlabReviewDecision maps GitLab reviewer and merge status info
// to a forge.ChangeReviewDecision.
//
// GitLab does not have a single "review decision" field like GitHub.
// We approximate it using:
//   - "approved" DetailedMergeStatus → ChangeReviewApproved
//   - non-empty reviewer list → ChangeReviewRequired
//   - otherwise → ChangeReviewNoReview
func gitlabReviewDecision(
	reviewers []*gitlab.BasicUser,
	detailedMergeStatus string,
) forge.ChangeReviewDecision {
	if detailedMergeStatus == "approved" {
		return forge.ChangeReviewApproved
	}

	if len(reviewers) > 0 {
		return forge.ChangeReviewRequired
	}

	return forge.ChangeReviewNoReview
}
