package bitbucket

import (
	"context"
	"fmt"
	"net/http"
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
	if err := doTestAction(ctx, repo, path); err != nil {
		return fmt.Errorf("merge PR: %w", err)
	}
	repo.log.Debug("Merged pull request", "pr", id.Number)
	return nil
}

func approvePR(
	ctx context.Context, repo *Repository, id *PR,
) error {
	path := prActionPath(repo, id.Number, "approve")
	if err := doTestAction(ctx, repo, path); err != nil {
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
	if err := doTestAction(ctx, repo, path); err != nil {
		return fmt.Errorf("decline PR: %w", err)
	}
	repo.log.Debug(
		"Declined pull request", "pr", id.Number,
	)
	return nil
}

func doTestAction(
	ctx context.Context,
	repo *Repository,
	path string,
) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, repo.forge.APIURL()+path, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "git-spice")
	if repo.token != nil && repo.token.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+repo.token.AccessToken)
	}

	httpClient := repo.http
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	return nil
}
