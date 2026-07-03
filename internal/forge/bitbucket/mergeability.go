package bitbucket

import (
	"context"

	"go.abhg.dev/gs/internal/forge"
)

// ChangeMergeability reports whether the pull request can be merged.
func (r *Repository) ChangeMergeability(
	ctx context.Context,
	id forge.ChangeID,
) (forge.ChangeMergeability, error) {
	pr := mustPR(id)
	pullRequest, err := r.gw.GetChange(ctx, pr.Number)
	if err != nil {
		return forge.ChangeMergeability{}, err
	}

	return pullRequest.Mergeability, nil
}
