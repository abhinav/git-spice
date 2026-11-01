package bitbucket

import (
	"context"
	"fmt"
	"net/http"

	"go.abhg.dev/gs/internal/forge"
)

func baseRepository(repo forge.Repository) *Repository {
	switch repo := repo.(type) {
	case *Repository:
		return repo
	case *reviewRepository:
		return repo.Repository
	default:
		panic(fmt.Sprintf("unexpected Bitbucket repository %T", repo))
	}
}

func cloudGW(repo forge.Repository) *testCloudGateway {
	return baseRepository(repo).gw.(*testCloudGateway)
}

func prActionPath(
	repo forge.Repository, prID int64, action string,
) string {
	gw := cloudGW(repo)
	return fmt.Sprintf(
		"/repositories/%s/%s/pullrequests/%d/%s",
		gw.workspace, gw.repo, prID, action,
	)
}

// MergeChange merges a pull request for integration tests.
func MergeChange(
	ctx context.Context, repo forge.Repository, id *PR,
) error {
	base := baseRepository(repo)
	if err := approvePR(ctx, repo, id); err != nil {
		base.log.Debug(
			"Approval failed (may not be required)",
			"err", err,
		)
	}

	path := prActionPath(repo, id.Number, "merge")
	if err := doTestAction(ctx, repo, path); err != nil {
		return fmt.Errorf("merge PR: %w", err)
	}
	base.log.Debug("Merged pull request", "pr", id.Number)
	return nil
}

func approvePR(
	ctx context.Context, repo forge.Repository, id *PR,
) error {
	path := prActionPath(repo, id.Number, "approve")
	if err := doTestAction(ctx, repo, path); err != nil {
		return fmt.Errorf("approve PR: %w", err)
	}
	return nil
}

// CloseChange declines a pull request for integration tests.
func CloseChange(
	ctx context.Context, repo forge.Repository, id *PR,
) error {
	path := prActionPath(repo, id.Number, "decline")
	if err := doTestAction(ctx, repo, path); err != nil {
		return fmt.Errorf("decline PR: %w", err)
	}
	baseRepository(repo).log.Debug(
		"Declined pull request", "pr", id.Number,
	)
	return nil
}

func doTestAction(
	ctx context.Context,
	repo forge.Repository,
	path string,
) error {
	gw := cloudGW(repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gw.apiURL+path, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "git-spice")
	if gw.token != nil && gw.token.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+gw.token.AccessToken)
	}

	httpClient := gw.http
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
