package bitbucket

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
)

// MergeChange merges an open pull request into its base branch.
//
// Bitbucket does not support expected-SHA assertions;
// opts.HeadHash is ignored.
func (r *Repository) MergeChange(
	ctx context.Context, fid forge.ChangeID,
	opts forge.MergeChangeOptions,
) error {
	id := mustPR(fid)

	var strategy string
	switch opts.Method {
	case forge.MergeMethodDefault:
	case forge.MergeMethodMerge:
		strategy = "merge_commit"
	case forge.MergeMethodSquash:
		strategy = "squash"
	case forge.MergeMethodRebase:
		strategy = "rebase_merge"
	default:
		r.log.Warn(
			"Unsupported merge method; using forge default",
			"method", opts.Method,
		)
	}
	if _, _, err := r.client.PullRequestMerge(
		ctx, r.workspace, r.repo, id.Number,
		&bitbucket.PullRequestMergeRequest{Strategy: strategy},
	); err != nil {
		return fmt.Errorf("merge pull request: %w", err)
	}

	r.log.Debug("Merged pull request", "pr", id.Number)
	return nil
}
