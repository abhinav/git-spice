package forgejo

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/forgejo"
)

// MergeChange merges an open pull request into its base branch.
func (r *Repository) MergeChange(
	ctx context.Context,
	id forge.ChangeID,
	opts forge.MergeChangeOptions,
) error {
	operation := mergeOperation(opts.Method)
	if opts.Method != forge.MergeMethodDefault && operation == "merge" {
		r.log.Warn(
			"Unsupported merge method; using forge default",
			"method", opts.Method,
		)
	}

	input := &forgejo.MergePullRequestOption{
		Do:           operation,
		HeadCommitID: opts.HeadHash.String(),
	}

	if _, _, err := r.client.PullRequestMerge(
		ctx,
		r.owner,
		r.repo,
		mustPR(id).Number,
		input,
	); err != nil {
		return fmt.Errorf("merge pull request: %w", err)
	}

	return nil
}

func mergeOperation(method forge.MergeMethod) string {
	switch method {
	case forge.MergeMethodSquash:
		return "squash"
	case forge.MergeMethodRebase:
		return "rebase"
	default:
		return "merge"
	}
}
