// Package github provides a wrapper around GitHub's APIs
// in a manner compliant with the [forge.Forge] interface.
package github

import (
	"cmp"
	"context"
	"fmt"
	"net/url"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgeurl"
	"go.abhg.dev/gs/internal/silog"
	"golang.org/x/oauth2"
)

// Default URLs for GitHub and its API.
const (
	DefaultURL    = "https://github.com"
	DefaultAPIURL = "https://api.github.com"
)

// Options defines command line options for the GitHub Forge.
// These are all hidden in the CLI,
// and are expected to be set only via environment variables.
type Options struct {
	// URL is the URL for GitHub.
	// Override this for testing or GitHub Enterprise.
	URL string `name:"github-url" hidden:"" config:"forge.github.url" env:"GITHUB_URL" help:"Base URL for GitHub web requests"`

	// APIURL is the URL for the GitHub API.
	// Override this for testing or GitHub Enterprise.
	APIURL string `name:"github-api-url" hidden:"" config:"forge.github.apiUrl" env:"GITHUB_API_URL" help:"Base URL for GitHub API requests"`

	// Token is a fixed token used to authenticate with GitHub.
	// This may be used to skip the login flow.
	Token string `name:"github-token" hidden:"" env:"GITHUB_TOKEN" help:"GitHub API token"`
}

// Forge builds a GitHub Forge.
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
	return f.Log.WithPrefix("github")
}

// URL returns the base URL configured for the GitHub Forge
// or the default URL if none is set.
func (f *Forge) URL() string {
	return cmp.Or(f.Options.URL, DefaultURL)
}

// APIURL returns the base API URL configured for the GitHub Forge
// or the default URL if none is set.
func (f *Forge) APIURL() string {
	if f.Options.APIURL != "" {
		return f.Options.APIURL
	}

	// If the API URL is not set, and base URL is NOT github.com,
	// assume API URL is $baseURL/api.
	if f.Options.URL != "" && f.Options.URL != DefaultURL {
		apiURL, err := url.JoinPath(f.Options.URL, "/api")
		if err == nil {
			return apiURL
		}
	}

	return DefaultAPIURL
}

// ID reports a unique key for this forge.
func (*Forge) ID() string { return "github" }

// CLIPlugin returns the CLI plugin for the GitHub Forge.
func (f *Forge) CLIPlugin() any { return &f.Options }

// ParseRemoteURL parses a GitHub remote URL and returns a [RepositoryID]
// if the URL matches.
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

// OpenRepository opens the GitHub repository that the given ID points to.
func (f *Forge) OpenRepository(ctx context.Context, tok forge.AuthenticationToken, id forge.RepositoryID) (forge.Repository, error) {
	rid := mustRepositoryID(id)

	tokenSource := tok.(*AuthenticationToken).tokenSource()
	ghc, err := newGitHubv4Client(ctx, f.APIURL(), tokenSource)
	if err != nil {
		return nil, fmt.Errorf("create GitHub client: %w", err)
	}

	return newRepository(ctx, f, rid.owner, rid.name, f.logger(), ghc, nil)
}

// RepositoryID is a unique identifier for a GitHub repository.
type RepositoryID struct {
	url   string // required
	owner string // required
	name  string // required
}

var _ forge.RepositoryID = (*RepositoryID)(nil)

func mustRepositoryID(id forge.RepositoryID) *RepositoryID {
	if rid, ok := id.(*RepositoryID); ok {
		return rid
	}
	panic(fmt.Sprintf("expected *RepositoryID, got %T", id))
}

// String returns a human-readable name for the repository ID.
func (rid *RepositoryID) String() string {
	return fmt.Sprintf("%s/%s", rid.owner, rid.name)
}

// ChangeURL returns a URL to view a change on GitHub.
func (rid *RepositoryID) ChangeURL(id forge.ChangeID) string {
	owner, repo := rid.owner, rid.name
	prNum := mustPR(id).Number
	return fmt.Sprintf("%s/%s/%s/pull/%d", rid.url, owner, repo, prNum)
}

func newGitHubv4Client(ctx context.Context, apiURL string, tokenSource oauth2.TokenSource) (*githubv4.Client, error) {
	graphQLAPIURL, err := url.JoinPath(apiURL, "/graphql")
	if err != nil {
		return nil, fmt.Errorf("build GraphQL API URL: %w", err)
	}

	httpClient := oauth2.NewClient(ctx, tokenSource)
	return newGitHubEnterpriseClient(graphQLAPIURL, httpClient), nil
}

func extractRepoInfo(githubURL, remoteURL string) (owner, repo string, err error) {
	baseURL, err := url.Parse(githubURL)
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
			"%v is not a GitHub URL: expected host %q, got %q",
			u, baseURL.Host, u.Host,
		)
	}

	owner, repo, ok := forgeurl.ExtractPath(u.Path)
	if !ok {
		return "", "", fmt.Errorf(
			"path %q does not contain a GitHub repository", u.Path,
		)
	}

	return owner, repo, nil
}
