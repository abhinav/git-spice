package azuredevops

import (
	"context"
	"fmt"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"go.abhg.dev/gs/internal/forge"
)

// ChangeMergeability reports whether the pull request can be merged.
func (r *Repository) ChangeMergeability(
	ctx context.Context,
	id forge.ChangeID,
) (forge.ChangeMergeability, error) {
	prID := mustPR(id).Number

	pr, err := r.client.gitClient.GetPullRequest(ctx, git.GetPullRequestArgs{
		Project:       strPtr(r.project()),
		RepositoryId:  strPtr(r.repositoryID()),
		PullRequestId: &prID,
	})
	if err != nil {
		return forge.ChangeMergeability{},
			fmt.Errorf("get pull request %d: %w", prID, err)
	}

	if pr.IsDraft != nil && *pr.IsDraft {
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonDraft,
		}, nil
	}

	if pr.MergeStatus == nil {
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityUnknown,
			Reason: forge.ChangeMergeabilityReasonUnknown,
		}, nil
	}

	switch *pr.MergeStatus {
	case git.PullRequestAsyncStatusValues.Succeeded:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityReady,
			Reason: forge.ChangeMergeabilityReasonUnknown,
		}, nil
	case git.PullRequestAsyncStatusValues.Conflicts:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonConflicts,
		}, nil
	case git.PullRequestAsyncStatusValues.RejectedByPolicy:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonPolicy,
		}, nil
	case git.PullRequestAsyncStatusValues.Failure:
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonUnknown,
		}, nil
	case git.PullRequestAsyncStatusValues.Queued:
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
