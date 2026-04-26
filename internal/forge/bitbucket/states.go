package bitbucket

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
)

// Bitbucket PR states.
const (
	stateOpen       = "OPEN"
	stateMerged     = "MERGED"
	stateDeclined   = "DECLINED"
	stateSuperseded = "SUPERSEDED"
)

// ChangeStatuses retrieves compact statuses for multiple pull requests.
func (r *Repository) ChangeStatuses(
	ctx context.Context,
	ids []forge.ChangeID,
) ([]forge.ChangeStatus, error) {
	statuses := make([]forge.ChangeStatus, len(ids))
	for i, id := range ids {
		status, err := r.getChangeStatus(ctx, mustPR(id).Number)
		if err != nil {
			return nil, fmt.Errorf("get state for PR #%d: %w", mustPR(id).Number, err)
		}
		statuses[i] = status
	}
	return statuses, nil
}

func (r *Repository) getChangeStatus(ctx context.Context, prID int64) (forge.ChangeStatus, error) {
	pr, err := r.getPullRequest(ctx, prID)
	if err != nil {
		return forge.ChangeStatus{}, err
	}
	return forge.ChangeStatus{
		State:    stateFromAPI(pr.State),
		HeadHash: extractHeadHash(pr),
	}, nil
}

func stateFromAPI(state string) forge.ChangeState {
	switch state {
	case stateOpen, "DRAFT":
		return forge.ChangeOpen
	case stateMerged:
		return forge.ChangeMerged
	case stateDeclined, stateSuperseded:
		return forge.ChangeClosed
	default:
		return forge.ChangeOpen
	}
}

func stateToAPI(state forge.ChangeState) string {
	switch state {
	case forge.ChangeOpen:
		return stateOpen
	case forge.ChangeMerged:
		return stateMerged
	case forge.ChangeClosed:
		return stateDeclined
	default:
		return stateOpen
	}
}
