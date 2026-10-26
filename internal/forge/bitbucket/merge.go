package bitbucket

import (
	"context"

	"go.abhg.dev/gs/internal/forge"
)

// MergeChange merges an open pull request into its base branch.
//
// Bitbucket does not support expected-SHA assertions;
// opts.HeadHash is ignored.
func (r *Repository) MergeChange(
	ctx context.Context,
	id forge.ChangeID,
	opts forge.MergeChangeOptions,
) error {
	return r.gw.MergeChange(ctx, mustPR(id).Number, opts.Method)
}
