package gitlab

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/gitlab"
)

// ChangesStates retrieves the states of the given changes in bulk.
func (r *Repository) ChangesStates(ctx context.Context, ids []forge.ChangeID) ([]forge.ChangeState, error) {
	mrIDs := make([]int64, len(ids))
	for i, id := range ids {
		mrIDs[i] = mustMR(id).Number
	}

	mergeRequests, _, err := r.client.MergeRequestList(
		ctx, r.repoID,
		&gitlab.ListProjectMergeRequestsOptions{IIDs: &mrIDs},
	)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	// create a map of MR IDs to MRs
	mrMap := make(map[int64]*gitlab.BasicMergeRequest)
	for _, mr := range mergeRequests {
		mrMap[mr.IID] = mr
	}

	states := make([]forge.ChangeState, len(mrIDs))
	for i, id := range mrIDs {
		mr := mrMap[id]
		switch mr.State {
		case "opened":
			states[i] = forge.ChangeOpen
		case "merged":
			states[i] = forge.ChangeMerged
		case "closed":
			states[i] = forge.ChangeClosed
		default:
			states[i] = forge.ChangeOpen // default to open for unknown states
		}
	}

	return states, nil
}
