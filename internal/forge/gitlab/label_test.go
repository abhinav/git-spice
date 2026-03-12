package gitlab

import (
	"context"

	"go.abhg.dev/gs/internal/forge"
)

func (r *Repository) ChangeLabels(ctx context.Context, id forge.ChangeID) ([]string, error) {
	mr := mustMR(id)
	mergeReq, _, err := r.client.MergeRequestGet(
		ctx, r.repoID, mr.Number, nil,
	)
	return mergeReq.Labels, err
}
