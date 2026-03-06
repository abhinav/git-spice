package bitbucket

import (
	"context"
	"fmt"
)

func prActionPath(
	repo *Repository, prID int64, action string,
) string {
	return fmt.Sprintf(
		"/repositories/%s/%s/pullrequests/%d/%s",
		repo.workspace, repo.repo, prID, action,
	)
}

// MergeChange merges a pull request.
// This is used by integration tests
// to put PRs in different states.
func MergeChange(
	ctx context.Context, repo *Repository, id *PR,
) error {
	if err := approvePR(ctx, repo, id); err != nil {
		repo.log.Debug(
			"Approval failed (may not be required)",
			"err", err,
		)
	}

	path := prActionPath(repo, id.Number, "merge")
	if err := repo.client.post(
		ctx, path, nil, nil,
	); err != nil {
		return fmt.Errorf("merge PR: %w", err)
	}
	repo.log.Debug("Merged pull request", "pr", id.Number)
	return nil
}

func approvePR(
	ctx context.Context, repo *Repository, id *PR,
) error {
	path := prActionPath(repo, id.Number, "approve")
	if err := repo.client.post(
		ctx, path, nil, nil,
	); err != nil {
		return fmt.Errorf("approve PR: %w", err)
	}
	return nil
}

// CloseChange declines (closes) a pull request
// without merging. This is used by integration tests
// to put PRs in different states.
func CloseChange(
	ctx context.Context, repo *Repository, id *PR,
) error {
	path := prActionPath(repo, id.Number, "decline")
	if err := repo.client.post(
		ctx, path, nil, nil,
	); err != nil {
		return fmt.Errorf("decline PR: %w", err)
	}
	repo.log.Debug(
		"Declined pull request", "pr", id.Number,
	)
	return nil
}
