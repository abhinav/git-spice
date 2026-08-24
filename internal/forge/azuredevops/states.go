package azuredevops

import (
	"context"
	"fmt"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"go.abhg.dev/gs/internal/forge"
	internalgit "go.abhg.dev/gs/internal/git"
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

		pr, err := r.client.gitClient.GetPullRequest(ctx, git.GetPullRequestArgs{
			Project:       new(r.project()),
			RepositoryId:  new(r.repositoryID()),
			PullRequestId: &prID,
		})
		if err != nil {
			return nil, fmt.Errorf("get pull request %d: %w", prID, err)
		}

		statuses[i].State = forge.ChangeOpen
		if pr.Status != nil {
			statuses[i].State = mapPRStatusToChangeState(*pr.Status)
		}
		if pr.LastMergeSourceCommit != nil &&
			pr.LastMergeSourceCommit.CommitId != nil {
			statuses[i].HeadHash = internalgit.Hash(
				*pr.LastMergeSourceCommit.CommitId,
			)
		}
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
func mapPRStatusToChangeState(status git.PullRequestStatus) forge.ChangeState {
	switch status {
	case git.PullRequestStatusValues.Active:
		return forge.ChangeOpen
	case git.PullRequestStatusValues.Completed:
		return forge.ChangeMerged
	case git.PullRequestStatusValues.Abandoned:
		return forge.ChangeClosed
	default:
		// NotSet or unknown status - treat as open.
		return forge.ChangeOpen
	}
}
