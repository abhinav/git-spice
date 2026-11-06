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
	return r.gw.ChangeMergeability(ctx, pr.Number)
}
