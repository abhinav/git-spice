package gitea

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.abhg.dev/gs/internal/forge"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
)

// MergeChange merges an open pull request into its base branch.
func (r *Repository) MergeChange(
	ctx context.Context,
	fid forge.ChangeID,
	opts forge.MergeChangeOptions,
) error {
	id := mustPR(fid)

	mergeOpts := &giteagw.MergePullRequestOption{
		Do: mergeMethod(opts.Method),
	}
	if opts.HeadHash != "" {
		mergeOpts.HeadCommitID = string(opts.HeadHash)
	}

	// Gitea computes mergeability asynchronously after a PR is created.
	// A 405 response ("Please try again later") means the check is still
	// in progress; retry a few times with exponential delays.
	const maxAttempts = 5
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		_, err := r.client.PullMerge(ctx, r.owner, r.repo, id.Number, mergeOpts)
		if err == nil {
			break
		}
		if !errors.Is(err, giteagw.ErrMergeNotReady) ||
			attempt == maxAttempts {
			return fmt.Errorf("merge pull request: %w", err)
		}

		delay := time.Second << (attempt - 1)
		r.log.Debug("Pull request not yet mergeable; retrying",
			"pr", id.Number,
			"attempt", attempt,
			"delay", delay,
		)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	r.log.Debug("Merged pull request", "pr", id.Number)
	return nil
}

func mergeMethod(m forge.MergeMethod) string {
	switch m {
	case forge.MergeMethodSquash:
		return "squash"
	case forge.MergeMethodRebase:
		return "rebase"
	default:
		return "merge"
	}
}
