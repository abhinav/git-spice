package gitlab

import (
	"context"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.abhg.dev/gs/internal/forge"
)

func (r *Repository) ChangeLabels(ctx context.Context, id forge.ChangeID) ([]string, error) {
	mr := mustMR(id)
	mergeReq, _, err := r.client.MergeRequests.GetMergeRequest(
		r.repoID, mr.Number, nil, gitlab.WithContext(ctx),
	)
	return []string(mergeReq.Labels), err
}
