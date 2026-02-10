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

// ChangesStates retrieves the states of multiple pull requests.
func (r *Repository) ChangesStates(
	ctx context.Context,
	ids []forge.ChangeID,
) ([]forge.ChangeState, error) {
	states := make([]forge.ChangeState, len(ids))
	for i, id := range ids {
		state, err := r.getChangeState(ctx, mustPR(id).Number)
		if err != nil {
			return nil, fmt.Errorf("get state for PR #%d: %w", mustPR(id).Number, err)
		}
		states[i] = state
	}
	return states, nil
}

func (r *Repository) getChangeState(ctx context.Context, prID int64) (forge.ChangeState, error) {
	pr, err := r.getPullRequest(ctx, prID)
	if err != nil {
		return 0, err
	}
	return stateFromAPI(pr.State), nil
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
