package github

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
)

// ChangeMergeability reports whether the pull request can be merged.
func (r *Repository) ChangeMergeability(
	ctx context.Context,
	fid forge.ChangeID,
) (forge.ChangeMergeability, error) {
	pr := mustPR(fid)
	gqlID, err := r.graphQLID(ctx, pr)
	if err != nil {
		return forge.ChangeMergeability{},
			fmt.Errorf("resolve PR ID: %w", err)
	}

	mergeability, err := r.gateway.PullRequestMergeability(ctx, gqlID)
	if err != nil {
		return forge.ChangeMergeability{},
			fmt.Errorf("query mergeability: %w", err)
	}

	return changeMergeabilityFromGitHub(
		mergeability.Mergeable,
		mergeability.MergeStateStatus,
		mergeability.IsDraft,
	), nil
}

func changeMergeabilityFromGitHub(
	mergeable github.MergeableState,
	mergeState github.MergeStateStatus,
	isDraft bool,
) forge.ChangeMergeability {
	if isDraft {
		// GitHub can report UNKNOWN merge state for drafts while its
		// mergeability calculation is still settling.
		// Use isDraft directly so drafts do not look like generic waiting.
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonDraft,
		}
	}

	switch mergeState {
	case github.MergeStateStatusClean, github.MergeStateStatusHasHooks,
		github.MergeStateStatusUnstable:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityReady,
			Reason: forge.ChangeMergeabilityReasonUnknown,
		}
	case github.MergeStateStatusDirty:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonConflicts,
		}
	case github.MergeStateStatusBehind:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonBehind,
		}
	case github.MergeStateStatusDraft:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonDraft,
		}
	case github.MergeStateStatusBlocked:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityWaiting,
			Reason: forge.ChangeMergeabilityReasonUnknown,
		}
	}

	switch mergeable {
	case github.MergeableStateConflicting:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonConflicts,
		}
	case github.MergeableStateMergeable, github.MergeableStateUnknown:
		// PullRequest.mergeable only reports the conflict calculation.
		// When mergeStateStatus is UNKNOWN or unsupported,
		// MERGEABLE does not prove that branch protection is satisfied.
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityWaiting,
			Reason: forge.ChangeMergeabilityReasonUnknown,
		}
	default:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityUnknown,
			Reason: forge.ChangeMergeabilityReasonUnknown,
		}
	}
}
