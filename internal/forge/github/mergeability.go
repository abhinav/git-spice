package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
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

	var q struct {
		Node struct {
			PullRequest struct {
				Mergeable        githubv4.MergeableState   `graphql:"mergeable"`
				MergeStateStatus githubv4.MergeStateStatus `graphql:"mergeStateStatus"`
				IsDraft          githubv4.Boolean          `graphql:"isDraft"`
			} `graphql:"... on PullRequest"`
		} `graphql:"node(id: $id)"`
	}
	if err := r.client.Query(ctx, &q, map[string]any{
		"id": gqlID,
	}); err != nil {
		return forge.ChangeMergeability{},
			fmt.Errorf("query mergeability: %w", err)
	}

	return changeMergeabilityFromGitHub(
		q.Node.PullRequest.Mergeable,
		q.Node.PullRequest.MergeStateStatus,
		bool(q.Node.PullRequest.IsDraft),
	), nil
}

func changeMergeabilityFromGitHub(
	mergeable githubv4.MergeableState,
	mergeState githubv4.MergeStateStatus,
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
	case githubv4.MergeStateStatusClean,
		githubv4.MergeStateStatusHasHooks,
		githubv4.MergeStateStatusUnstable:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityReady,
			Reason: forge.ChangeMergeabilityReasonUnknown,
		}
	case githubv4.MergeStateStatusDirty:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonConflicts,
		}
	case githubv4.MergeStateStatusBehind:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonBehind,
		}
	case githubv4.MergeStateStatusDraft:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonDraft,
		}
	case githubv4.MergeStateStatusBlocked:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonUnknown,
		}
	}

	switch mergeable {
	case githubv4.MergeableStateConflicting:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonConflicts,
		}
	case githubv4.MergeableStateMergeable,
		githubv4.MergeableStateUnknown:
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
