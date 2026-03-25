package gitlab

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/gitlab"
)

// MergeChange merges an open merge request into its base branch.
func (r *Repository) MergeChange(
	ctx context.Context, fid forge.ChangeID,
	opts forge.MergeChangeOptions,
) error {
	id := mustMR(fid)

	mrOpts := &gitlab.AcceptMergeRequestOptions{}
	if opts.HeadHash != "" {
		sha := string(opts.HeadHash)
		mrOpts.SHA = &sha
	}

	if _, _, err := r.client.MergeRequestAccept(
		ctx, r.repoID, id.Number, mrOpts,
	); err != nil {
		return fmt.Errorf("merge merge request: %w", err)
	}

	r.log.Debug("Merged merge request", "mr", id.Number)
	return nil
}
