package gitlab

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/gitlab"
)

var _detailedMergeStatusMergeability = map[string]forge.ChangeMergeability{
	gitlab.DetailedMergeStatusMergeable: {
		State: forge.ChangeMergeabilityReady,
	},
	gitlab.DetailedMergeStatusApprovalsSyncing: {
		State:  forge.ChangeMergeabilityWaiting,
		Reason: forge.ChangeMergeabilityReasonReview,
	},
	gitlab.DetailedMergeStatusChecking: {
		State: forge.ChangeMergeabilityWaiting,
	},
	gitlab.DetailedMergeStatusPreparing: {
		State: forge.ChangeMergeabilityWaiting,
	},
	gitlab.DetailedMergeStatusUnchecked: {
		State: forge.ChangeMergeabilityWaiting,
	},
	gitlab.DetailedMergeStatusCIStillRunning: {
		State:  forge.ChangeMergeabilityWaiting,
		Reason: forge.ChangeMergeabilityReasonChecks,
	},
	gitlab.DetailedMergeStatusCIMustPass: {
		State:  forge.ChangeMergeabilityBlocked,
		Reason: forge.ChangeMergeabilityReasonChecks,
	},
	gitlab.DetailedMergeStatusSecurityPolicyPipelineCheck: {
		State:  forge.ChangeMergeabilityBlocked,
		Reason: forge.ChangeMergeabilityReasonChecks,
	},
	gitlab.DetailedMergeStatusStatusChecksMustPass: {
		State:  forge.ChangeMergeabilityBlocked,
		Reason: forge.ChangeMergeabilityReasonChecks,
	},
	gitlab.DetailedMergeStatusConflict: {
		State:  forge.ChangeMergeabilityBlocked,
		Reason: forge.ChangeMergeabilityReasonConflicts,
	},
	gitlab.DetailedMergeStatusDiscussionsNotResolved: {
		State:  forge.ChangeMergeabilityBlocked,
		Reason: forge.ChangeMergeabilityReasonDiscussions,
	},
	gitlab.DetailedMergeStatusDraftStatus: {
		State:  forge.ChangeMergeabilityBlocked,
		Reason: forge.ChangeMergeabilityReasonDraft,
	},
	gitlab.DetailedMergeStatusNeedRebase: {
		State:  forge.ChangeMergeabilityBlocked,
		Reason: forge.ChangeMergeabilityReasonBehind,
	},
	gitlab.DetailedMergeStatusNotApproved: {
		State:  forge.ChangeMergeabilityBlocked,
		Reason: forge.ChangeMergeabilityReasonReview,
	},
	gitlab.DetailedMergeStatusRequestedChanges: {
		State:  forge.ChangeMergeabilityBlocked,
		Reason: forge.ChangeMergeabilityReasonReview,
	},
	gitlab.DetailedMergeStatusCommitsStatus: {
		State:  forge.ChangeMergeabilityBlocked,
		Reason: forge.ChangeMergeabilityReasonPolicy,
	},
	gitlab.DetailedMergeStatusJiraAssociationMissing: {
		State:  forge.ChangeMergeabilityBlocked,
		Reason: forge.ChangeMergeabilityReasonPolicy,
	},
	gitlab.DetailedMergeStatusMergeRequestBlocked: {
		State:  forge.ChangeMergeabilityBlocked,
		Reason: forge.ChangeMergeabilityReasonPolicy,
	},
	gitlab.DetailedMergeStatusMergeTime: {
		State:  forge.ChangeMergeabilityBlocked,
		Reason: forge.ChangeMergeabilityReasonPolicy,
	},
	gitlab.DetailedMergeStatusNotOpen: {
		State:  forge.ChangeMergeabilityBlocked,
		Reason: forge.ChangeMergeabilityReasonPolicy,
	},
	gitlab.DetailedMergeStatusSecurityPolicyViolations: {
		State:  forge.ChangeMergeabilityBlocked,
		Reason: forge.ChangeMergeabilityReasonPolicy,
	},
	gitlab.DetailedMergeStatusLockedPaths: {
		State:  forge.ChangeMergeabilityBlocked,
		Reason: forge.ChangeMergeabilityReasonPolicy,
	},
	gitlab.DetailedMergeStatusLockedLFSFiles: {
		State:  forge.ChangeMergeabilityBlocked,
		Reason: forge.ChangeMergeabilityReasonPolicy,
	},
	gitlab.DetailedMergeStatusTitleRegex: {
		State:  forge.ChangeMergeabilityBlocked,
		Reason: forge.ChangeMergeabilityReasonPolicy,
	},
}

// ChangeMergeability reports whether the merge request can be merged.
func (r *Repository) ChangeMergeability(
	ctx context.Context,
	fid forge.ChangeID,
) (forge.ChangeMergeability, error) {
	id := mustMR(fid)
	mr, _, err := r.client.MergeRequestGet(
		ctx, r.repoID, id.Number, nil,
	)
	if err != nil {
		return forge.ChangeMergeability{}, fmt.Errorf(
			"get merge request: %w", err,
		)
	}

	if mr.DetailedMergeStatus == gitlab.DetailedMergeStatusNeedRebase &&
		mr.HasConflicts {
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonConflicts,
		}, nil
	}

	if mergeability, ok := _detailedMergeStatusMergeability[mr.DetailedMergeStatus]; ok {
		return mergeability, nil
	}

	return forge.ChangeMergeability{
		State: forge.ChangeMergeabilityUnknown,
	}, nil
}
