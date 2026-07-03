package bitbucket

import (
	"context"
	"fmt"
	"net/http"

	"go.abhg.dev/gs/internal/forge"
	bitbucketapi "go.abhg.dev/gs/internal/gateway/bitbucket/cloud"
	"go.abhg.dev/gs/internal/git"
)

func cloudGW(repo *Repository) *testCloudGateway {
	return repo.gw.(*testCloudGateway)
}

func prActionPath(
	repo *Repository, prID int64, action string,
) string {
	gw := cloudGW(repo)
	return fmt.Sprintf(
		"/repositories/%s/%s/pullrequests/%d/%s",
		gw.workspace, gw.repo, prID, action,
	)
}

// MergeChange merges a pull request for integration tests.
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

// SetChangeChecksState sets a synthetic build status for integration tests.
func SetChangeChecksState(
	ctx context.Context,
	repo *Repository,
	headHash git.Hash,
	state forge.ChangeCheckState,
) error {
	gw := cloudGW(repo)
	_, _, err := gw.client.CommitStatusCreate(
		ctx,
		gw.workspace,
		gw.repo,
		headHash.String(),
		&bitbucketapi.CommitStatusCreateRequest{
			Key:         "git-spice-integration",
			State:       bitbucketStatusState(state),
			Description: "Synthetic status for git-spice integration tests",
		},
	)
	if err != nil {
		return fmt.Errorf("set commit status: %w", err)
	}
	return nil
}

func bitbucketStatusState(state forge.ChangeCheckState) string {
	switch state {
	case forge.ChangeCheckPending:
		return bitbucketapi.CommitStatusInProgress
	case forge.ChangeCheckPassed:
		return bitbucketapi.CommitStatusSuccessful
	case forge.ChangeCheckFailed:
		return bitbucketapi.CommitStatusFailed
	default:
		return bitbucketapi.CommitStatusFailed
	}
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

// CloseChange declines a pull request for integration tests.
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
