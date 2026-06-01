package forgejo

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/forgejo"
	"go.abhg.dev/gs/internal/git"
)

// ChangeStatuses retrieves compact statuses for the given changes.
func (r *Repository) ChangeStatuses(
	ctx context.Context,
	ids []forge.ChangeID,
) ([]forge.ChangeStatus, error) {
	statuses := make([]forge.ChangeStatus, len(ids))
	for i, id := range ids {
		pr, _, err := r.client.PullRequestGet(
			ctx, r.owner, r.repo, mustPR(id).Number,
		)
		if err != nil {
			return nil, fmt.Errorf("get pull request %v: %w", id, err)
		}
		statuses[i] = forge.ChangeStatus{
			State:    forgeChangeState(pr.State, pr.Merged),
			HeadHash: pullRequestHeadHash(pr),
		}
	}
	return statuses, nil
}

// ChangeChecksState reports the aggregate commit status
// for the given pull request.
func (r *Repository) ChangeChecksState(
	ctx context.Context,
	id forge.ChangeID,
) (forge.ChecksState, error) {
	pr, _, err := r.client.PullRequestGet(
		ctx, r.owner, r.repo, mustPR(id).Number,
	)
	if err != nil {
		return 0, fmt.Errorf("get pull request: %w", err)
	}
	if pr.Head == nil || pr.Head.SHA == "" {
		return forge.ChecksPassed, nil
	}

	status, _, err := r.client.CombinedStatusGet(
		ctx, r.owner, r.repo, pr.Head.SHA,
	)
	if err != nil {
		return 0, fmt.Errorf("get combined status: %w", err)
	}
	if len(status.Statuses) == 0 {
		return forge.ChecksPassed, nil
	}
	return checksState(status.State), nil
}

func forgeChangeState(state string, merged bool) forge.ChangeState {
	if merged {
		return forge.ChangeMerged
	}
	switch state {
	case "open":
		return forge.ChangeOpen
	case "closed":
		return forge.ChangeClosed
	case "merged":
		return forge.ChangeMerged
	default:
		return 0
	}
}

func checksState(state forgejo.CommitStatusState) forge.ChecksState {
	switch state {
	case forgejo.CommitStatusSuccess, forgejo.CommitStatusWarning:
		return forge.ChecksPassed
	case forgejo.CommitStatusFailure, forgejo.CommitStatusError:
		return forge.ChecksFailed
	default:
		return forge.ChecksPending
	}
}

func pullRequestHeadHash(pr *forgejo.PullRequest) git.Hash {
	if pr.Head == nil {
		return ""
	}
	return git.Hash(pr.Head.SHA)
}
