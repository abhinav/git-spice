package bitbucket

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
)

// MergeChange merges an open pull request into its base branch.
//
// Bitbucket does not support expected-SHA assertions;
// opts.HeadHash is ignored.
func (r *Repository) MergeChange(
	ctx context.Context, fid forge.ChangeID,
	_ forge.MergeChangeOptions,
) error {
	id := mustPR(fid)
	if _, _, err := r.client.PullRequestMerge(
		ctx, r.workspace, r.repo, id.Number,
	); err != nil {
		return fmt.Errorf("merge pull request: %w", err)
	}

	r.log.Debug("Merged pull request", "pr", id.Number)
	return nil
}
