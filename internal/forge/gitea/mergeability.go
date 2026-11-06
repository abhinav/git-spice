package gitea

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
)

// ChangeMergeability reports whether the pull request can be merged.
func (r *Repository) ChangeMergeability(
	ctx context.Context,
	fid forge.ChangeID,
) (forge.ChangeMergeability, error) {
	id := mustPR(fid)

	pr, _, err := r.client.PullGet(ctx, r.owner, r.repo, id.Number)
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

	if pr.Mergeable == nil {
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityUnknown,
			Reason: forge.ChangeMergeabilityReasonUnknown,
		}, nil
	}

	if *pr.Mergeable {
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
