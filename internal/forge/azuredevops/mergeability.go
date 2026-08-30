package azuredevops

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/azuredevops"
)

// ChangeMergeability reports whether the pull request can be merged.
func (r *Repository) ChangeMergeability(
	ctx context.Context,
	id forge.ChangeID,
) (forge.ChangeMergeability, error) {
	prID := mustPR(id).Number

	pr, err := r.gateway.PullRequest(ctx, r.project(), r.repositoryID(), prID)
	if err != nil {
		return forge.ChangeMergeability{},
			fmt.Errorf("get pull request %d: %w", prID, err)
	}

	if pr.Draft {
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonDraft,
		}, nil
	}

	switch pr.MergeStatus {
	case azuredevops.MergeStatusSucceeded:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityReady,
			Reason: forge.ChangeMergeabilityReasonUnknown,
		}, nil
	case azuredevops.MergeStatusConflicts:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonConflicts,
		}, nil
	case azuredevops.MergeStatusRejectedByPolicy:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonPolicy,
		}, nil
	case azuredevops.MergeStatusFailure:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonUnknown,
		}, nil
	case azuredevops.MergeStatusQueued:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityWaiting,
			Reason: forge.ChangeMergeabilityReasonUnknown,
		}, nil
	default:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityUnknown,
			Reason: forge.ChangeMergeabilityReasonUnknown,
		}, nil
	}
}
