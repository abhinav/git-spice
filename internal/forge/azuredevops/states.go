package azuredevops

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/azuredevops"
	"go.abhg.dev/gs/internal/git"
)

// ChangeStatuses retrieves compact statuses for the given changes in bulk.
func (r *Repository) ChangeStatuses(
	ctx context.Context,
	ids []forge.ChangeID,
) ([]forge.ChangeStatus, error) {
	statuses := make([]forge.ChangeStatus, len(ids))

	// Azure DevOps doesn't have a batch API for fetching PR states,
	// so we need to fetch each PR individually.
	for i, id := range ids {
		prID := mustPR(id).Number

		pr, err := r.gateway.PullRequest(ctx, r.project(), r.repositoryID(), prID)
		if err != nil {
			return nil, fmt.Errorf("get pull request %d: %w", prID, err)
		}

		statuses[i].State = mapPRStatusToChangeState(pr.Status)
		statuses[i].HeadHash = git.Hash(pr.HeadCommit)
	}

	return statuses, nil
}

// ChangeChecks reports CI/checks for the given pull request.
//
// Azure DevOps build/check integration is not implemented yet,
// so this always returns an empty slice.
func (r *Repository) ChangeChecks(
	_ context.Context,
	_ forge.ChangeID,
) ([]forge.ChangeCheck, error) {
	return nil, nil
}

// mapPRStatusToChangeState maps an Azure DevOps PR status
// to a forge.ChangeState.
func mapPRStatusToChangeState(status azuredevops.PullRequestStatus) forge.ChangeState {
	switch status {
	case azuredevops.PullRequestStatusActive:
		return forge.ChangeOpen
	case azuredevops.PullRequestStatusCompleted:
		return forge.ChangeMerged
	case azuredevops.PullRequestStatusAbandoned:
		return forge.ChangeClosed
	default:
		// NotSet or unknown status - treat as open.
		return forge.ChangeOpen
	}
}
