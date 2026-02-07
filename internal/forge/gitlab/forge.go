// Package gitlab provides a wrapper around GitLab's APIs
// in a manner compliant with the [forge.Forge] interface.
package gitlab

import (
	"cmp"
	"context"
	"fmt"
	"net/url"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgeurl"
	"go.abhg.dev/gs/internal/silog"
)

const (
	// DefaultURL Default URLs for GitLab and its API.
	DefaultURL = "https://gitlab.com"
)

// Options defines command line options for the GitLab Forge.
// These are all hidden in the CLI,
// and are expected to be set only via environment variables.
type Options struct {
	// URL is the URL for GitLab.
	// Override this for testing or if you use an on premise GitLab instance.
	URL string `name:"gitlab-url" hidden:"" config:"forge.gitlab.url" env:"GITLAB_URL" help:"Base URL for GitLab web requests"`

	// APIURL is the URL for GitLab's API.
	// Override this for testing or if you use an on premise GitLab instance with non-standard port url.
	APIURL string `name:"gitlab-api-url" hidden:"" config:"forge.gitlab.apiURL" env:"GITLAB_API_URL" help:"Base URL for GitLab API requests"`

	// Token is a fixed token used to authenticate with GitLab.
	// This may be used to skip the login flow.
	Token string `name:"gitlab-token" hidden:"" env:"GITLAB_TOKEN" help:"GitLab API token"`

	// ClientID is the OAuth client ID for GitLab OAuth device flow.
	// This should be used if the GitLab instance is Self Managed.
	ClientID string `name:"gitlab-oauth-client-id" hidden:"" env:"GITLAB_OAUTH_CLIENT_ID" config:"forge.gitlab.oauth.clientID" help:"GitLab OAuth client ID"`

	// RemoveSourceBranch specifies whether a branch should be deleted
	// after its Merge Request is merged.
	RemoveSourceBranch bool `name:"gitlab-remove-source-branch" hidden:"" config:"forge.gitlab.removeSourceBranch" default:"true" help:"Remove source branch after merging a merge request"`
}

// Forge builds a GitLab Forge.
type Forge struct {
	Options Options

	// Log specifies the logger to use.
	Log *silog.Logger
}

var _ forge.Forge = (*Forge)(nil)

func (f *Forge) logger() *silog.Logger {
	if f.Log == nil {
		return silog.Nop()
	}
	return f.Log.WithPrefix("gitlab")
}

// URL returns the base URL configured for the GitLab Forge
// or the default URL if none is set.
func (f *Forge) URL() string {
	return cmp.Or(f.Options.URL, DefaultURL)
}

// APIURL returns the base API URL configured for the GitHub Forge
// or the default URL if none is set.
func (f *Forge) APIURL() string {
	return cmp.Or(f.Options.APIURL, f.URL())
}

// ID reports a unique key for this forge.
func (*Forge) ID() string { return "gitlab" }

// CLIPlugin returns the CLI plugin for the GitLab Forge.
func (f *Forge) CLIPlugin() any { return &f.Options }

// ParseRemoteURL parses the given  remote URL and returns a [RepositoryID]
// for the GitLab repository it points to.
//
// It returns [ErrUnsupportedURL] if the remote URL is not a valid GitLab URL.
func (f *Forge) ParseRemoteURL(remoteURL string) (forge.RepositoryID, error) {
	owner, repo, err := extractRepoInfo(f.URL(), remoteURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", forge.ErrUnsupportedURL, err)
	}

	return &RepositoryID{
		url:   f.URL(),
		owner: owner,
		name:  repo,
	}, nil
}

// OpenRepository opens the GitLab repository that the given ID points to.
func (f *Forge) OpenRepository(ctx context.Context, token forge.AuthenticationToken, id forge.RepositoryID) (forge.Repository, error) {
	rid := mustRepositoryID(id)

	glc, err := newGitLabClient(ctx, f.APIURL(), token.(*AuthenticationToken))
	if err != nil {
		return nil, fmt.Errorf("create GitLab client: %w", err)
	}

	return newRepository(ctx, f, rid.owner, rid.name, f.logger(), glc, &repositoryOptions{
		RemoveSourceBranchOnMerge: f.Options.RemoveSourceBranch,
	})
}

// RepositoryID is a unique identifier for a GitLab repository.
type RepositoryID struct {
	url   string // required
	owner string // required
	name  string // required
}

var _ forge.RepositoryID = (*RepositoryID)(nil)

func mustRepositoryID(id forge.RepositoryID) *RepositoryID {
	rid, ok := id.(*RepositoryID)
	if ok {
		return rid
	}
	panic(fmt.Sprintf("expected *RepositoryID, got %T", id))
}

// String returns a human-readable name for the repository ID.
func (rid *RepositoryID) String() string {
	return fmt.Sprintf("%s/%s", rid.owner, rid.name)
}

// ChangeURL returns the URL for a Change hosted on GitLab.
func (rid *RepositoryID) ChangeURL(id forge.ChangeID) string {
	owner, repo := rid.owner, rid.name
	mrNum := mustMR(id).Number
	return fmt.Sprintf("%s/%s/%s/-/merge_requests/%v", rid.url, owner, repo, mrNum)
}

func extractRepoInfo(gitlabURL, remoteURL string) (owner, repo string, err error) {
	baseURL, err := url.Parse(gitlabURL)
	if err != nil {
		return "", "", fmt.Errorf("bad base URL: %w", err)
	}

	u, err := forgeurl.Parse(remoteURL)
	if err != nil {
		return "", "", err
	}

	forgeurl.StripDefaultPort(baseURL, u)

	if !forgeurl.MatchesHost(baseURL, u) {
		return "", "", fmt.Errorf(
			"%v is not a GitLab URL: expected host %q, got %q",
			u, baseURL.Host, u.Host,
		)
	}

	owner, repo, ok := forgeurl.ExtractPath(u.Path)
	if !ok {
		return "", "", fmt.Errorf(
			"path %q does not contain a GitLab repository", u.Path,
		)
	}

	return owner, repo, nil
}
