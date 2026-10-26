package forgejo

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
)

// ChangeMergeability reports whether the pull request can be merged.
func (r *Repository) ChangeMergeability(
	ctx context.Context,
	id forge.ChangeID,
) (forge.ChangeMergeability, error) {
	pr, _, err := r.client.PullRequestGet(
		ctx, r.owner, r.repo, mustPR(id).Number,
	)
	if err != nil {
		return forge.ChangeMergeability{},
			fmt.Errorf("get pull request: %w", err)
	}

	if pr.Draft {
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonDraft,
		}, nil
	}

	if pr.Mergeable {
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityReady,
			Reason: forge.ChangeMergeabilityReasonUnknown,
		}, nil
	}

	return forge.ChangeMergeability{
		State:  forge.ChangeMergeabilityBlocked,
		Reason: forge.ChangeMergeabilityReasonConflicts,
	}, nil
}
