package bitbucket

import (
	"context"

	"go.abhg.dev/gs/internal/forge"
)

// ChangeMergeability reports whether the pull request can be merged.
func (r *Repository) ChangeMergeability(
	ctx context.Context,
	id forge.ChangeID,
) (forge.ChangeMergeability, error) {
	pr := mustPR(id)
	pullRequest, err := r.getPullRequest(ctx, pr.Number)
	if err != nil {
		return forge.ChangeMergeability{}, err
	}

	return mergeabilityFromAPI(pullRequest.Mergeable, pullRequest.Queued), nil
}

func mergeabilityFromAPI(mergeable *bool, queued bool) forge.ChangeMergeability {
	// Bitbucket Cloud reports the mergeability decision,
	// but not the reason behind a blocked or waiting decision.
	result := forge.ChangeMergeability{
		Reason: forge.ChangeMergeabilityReasonUnknown,
	}
	switch {
	case mergeable == nil && queued:
		result.State = forge.ChangeMergeabilityWaiting
	case mergeable == nil:
		result.State = forge.ChangeMergeabilityUnknown
	case queued:
		result.State = forge.ChangeMergeabilityWaiting
	case *mergeable:
		result.State = forge.ChangeMergeabilityReady
	default:
		result.State = forge.ChangeMergeabilityBlocked
	}

	return result
}
