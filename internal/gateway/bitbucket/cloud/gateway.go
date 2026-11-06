package cloud

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
)

// Gateway implements [bitbucket.Gateway] for Bitbucket Cloud
// on top of its REST API 2.0.
//
// Bitbucket Cloud supports every optional gateway capability.
type Gateway struct {
	client *Client

	url             string // base web URL (e.g. "https://bitbucket.org")
	workspace, repo string
	log             *silog.Logger

	// defaultBranchName memoizes the default branch name;
	// see defaultBranch.
	defaultBranchMu       sync.Mutex
	defaultBranchName     string
	defaultBranchResolved bool
}

var _ bitbucket.Gateway = (*Gateway)(nil)

// New builds a Gateway
// for the Bitbucket Cloud repository {workspace}/{repo},
// talking to the REST API rooted at apiURL.
//
// baseURL is the web URL of the Bitbucket Cloud instance,
// used to build links for human consumption.
// httpClient transports all API requests;
// integration tests inject a recording client here.
func New(
	apiURL, baseURL string,
	workspace, repo string,
	log *silog.Logger,
	token *Token,
	httpClient *http.Client,
) (*Gateway, error) {
	if token == nil {
		return nil, errors.New("nil authentication token")
	}

	client, err := NewClient(
		StaticTokenSource(Token{
			AccessToken: token.AccessToken,
		}),
		&ClientOptions{
			BaseURL:    apiURL,
			HTTPClient: httpClient,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("create Bitbucket client: %w", err)
	}

	return &Gateway{
		client:    client,
		url:       baseURL,
		workspace: workspace,
		repo:      repo,
		log:       log,
	}, nil
}

// Product returns the product name used in user-facing warnings.
func (*Gateway) Product() string { return "Bitbucket" }

// ChangeURL returns the web URL
// for viewing the pull request with the given number.
func (g *Gateway) ChangeURL(number int64) string {
	return fmt.Sprintf(
		"%s/%s/%s/pull-requests/%d",
		g.url, g.workspace, g.repo, number,
	)
}

// ChangeTemplate fetches the contents of the change template file
// at the given path on the repository's default branch.
func (g *Gateway) ChangeTemplate(
	ctx context.Context,
	path string,
) (string, error) {
	branch, err := g.defaultBranch(ctx)
	if err != nil {
		return "", fmt.Errorf("get default branch: %w", err)
	}
	if branch == "" {
		// An empty repository has no default branch, and no templates.
		return "", fmt.Errorf("empty repository: %w", forge.ErrNotFound)
	}

	body, _, err := g.client.SourceFileGet(
		ctx, g.workspace, g.repo, branch, path,
	)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", fmt.Errorf(
				"template %q not found: %w", path, forge.ErrNotFound,
			)
		}
		return "", err
	}
	return string(body), nil
}

// defaultBranch resolves and memoizes
// the repository's default branch name.
// Only successful lookups are cached, so failures are retried;
// an empty name is a successful lookup
// reporting that the repository is empty.
func (g *Gateway) defaultBranch(ctx context.Context) (string, error) {
	g.defaultBranchMu.Lock()
	defer g.defaultBranchMu.Unlock()
	if g.defaultBranchResolved {
		return g.defaultBranchName, nil
	}

	repo, _, err := g.client.RepositoryGet(ctx, g.workspace, g.repo)
	if err != nil {
		return "", fmt.Errorf("get repository: %w", err)
	}
	g.defaultBranchName = repo.MainBranch.Name
	g.defaultBranchResolved = true
	return g.defaultBranchName, nil
}

// ListCommitChecks reports the CI checks
// recorded for the given commit.
func (g *Gateway) ListCommitChecks(
	ctx context.Context,
	commit git.Hash,
) ([]forge.ChangeCheck, error) {
	var statuses []CommitStatus
	opt := &CommitStatusListOptions{}
	for {
		page, resp, err := g.client.CommitStatusList(
			ctx, g.workspace, g.repo, commit.String(), opt,
		)
		if err != nil {
			return nil, fmt.Errorf("get commit statuses: %w", err)
		}

		statuses = append(statuses, page.Values...)
		if resp.NextURL == "" {
			break
		}
		opt.PageURL = resp.NextURL
	}

	checks := make([]forge.ChangeCheck, 0, len(statuses))
	for i, s := range statuses {
		check := forge.ChangeCheck{Name: s.Key}
		if check.Name == "" {
			check.Name = fmt.Sprintf("Bitbucket build status %d", i+1)
		}
		switch s.State {
		case CommitStatusFailed,
			CommitStatusStopped:
			check.State = forge.ChangeCheckFailed
		case CommitStatusInProgress:
			check.State = forge.ChangeCheckPending
		default:
			check.State = forge.ChangeCheckPassed
		}
		checks = append(checks, check)
	}
	return checks, nil
}
