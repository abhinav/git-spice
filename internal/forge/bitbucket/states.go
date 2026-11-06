package bitbucket

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
)

// ChangeStatuses retrieves compact statuses for multiple pull requests.
func (r *Repository) ChangeStatuses(
	ctx context.Context,
	ids []forge.ChangeID,
) ([]forge.ChangeStatus, error) {
	statuses := make([]forge.ChangeStatus, len(ids))
	for i, id := range ids {
		num := mustPR(id).Number
		pr, err := r.gw.GetChange(ctx, num)
		if err != nil {
			return nil, fmt.Errorf("get state for PR #%d: %w", num, err)
		}
		statuses[i] = forge.ChangeStatus{
			State:    pr.State,
			HeadHash: pr.HeadHash,
		}
	}
	return statuses, nil
}
