package github

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
	"go.abhg.dev/gs/internal/git"
)

// ChangeStatuses retrieves compact statuses for the given changes in bulk.
func (r *Repository) ChangeStatuses(ctx context.Context, ids []forge.ChangeID) ([]forge.ChangeStatus, error) {
	gqlIDs := make([]github.ID, len(ids))
	for i, id := range ids {
		pr := mustPR(id)
		gqlID, err := r.graphQLID(ctx, pr)
		if err != nil {
			return nil, fmt.Errorf("resolve ID %v: %w", id, err)
		}
		gqlIDs[i] = gqlID
	}

	githubStatuses, err := r.gateway.ChangeStatuses(ctx, gqlIDs)
	if err != nil {
		return nil, fmt.Errorf("retrieve change states: %w", err)
	}

	statuses := make([]forge.ChangeStatus, len(ids))
	for i, status := range githubStatuses {
		statuses[i] = forge.ChangeStatus{
			State:    forgeChangeState(status.State),
			HeadHash: git.Hash(status.HeadRefOID),
		}
	}

	return statuses, nil
}
