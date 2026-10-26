package bitbucket

import (
	"context"

	"go.abhg.dev/gs/internal/forge"
)

// ChangeChecks reports build statuses for the given pull request.
func (r *Repository) ChangeChecks(
	ctx context.Context, fid forge.ChangeID,
) ([]forge.ChangeCheck, error) {
	id := mustPR(fid)
	pr, err := r.gw.GetChange(ctx, id.Number)
	if err != nil {
		return nil, err
	}

	if pr.HeadHash == "" {
		return nil, nil
	}

	return r.gw.ListCommitChecks(ctx, pr.HeadHash)
}
