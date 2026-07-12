// Package github provides a wrapper around GitHub's APIs
// in a manner compliant with the [forge.Forge] interface.
package github

import (
	"cmp"
	"context"
	"fmt"
	"net/url"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
	"go.abhg.dev/gs/internal/git/giturl"
	"go.abhg.dev/gs/internal/silog"
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

// Definition configures GitHub forge instances.
type Definition struct {
	changeMetadataCodec

	// Options stores CLI and environment configuration.
	Options Options

	// Log specifies the logger to use.
	Log *silog.Logger
}

var (
	_ forge.Definition = (*Definition)(nil)
	_ forge.Forge      = (*Forge)(nil)
)

// ID reports a unique key for this forge.
func (*Definition) ID() string { return "github" }

// BaseURL reports the GitHub web URL used for host matching.
func (d *Definition) BaseURL() string {
	return cmp.Or(d.Options.URL, DefaultURL)
}

// CLIPlugin returns the CLI plugin for the GitHub Forge.
func (d *Definition) CLIPlugin() any { return &d.Options }

// New constructs a GitHub Forge from the configured options.
func (d *Definition) New(remoteURL *giturl.URL) (forge.Forge, error) {
	if err := forge.ValidateRemoteURL(d.Options.URL, remoteURL); err != nil {
		return nil, err
	}

	return &Forge{
		Options: d.Options,
		baseURL: d.BaseURL(),
		Log:     d.Log,
	}, nil
}

// Forge provides a GitHub forge instance.
type Forge struct {
	changeMetadataCodec

	Options Options
	baseURL string

	// Log specifies the logger to use.
	Log *silog.Logger
}

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

// BaseURL reports the GitHub web URL used for host matching and links.
func (f *Forge) BaseURL() string {
	return f.baseURL
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

// ParseRepositoryPath parses a GitHub repository path and returns a [RepositoryID]
// if the path identifies a repository.
func (f *Forge) ParseRepositoryPath(path string) (forge.RepositoryID, error) {
	owner, repo, err := extractRepoInfo(path)
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

	tokenSource, err := f.tokenSource(tok.(*AuthenticationToken))
	if err != nil {
		return nil, err
	}

	gatewayTokens := newGatewayTokenSource(tokenSource)
	gatewayClient, err := github.NewGateway(f.APIURL(), nil, gatewayTokens)
	if err != nil {
		return nil, fmt.Errorf("create GitHub gateway: %w", err)
	}

	return newRepository(ctx, f, rid.owner, rid.name, f.logger(), gatewayClient, "")
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

func extractRepoInfo(path string) (owner, repo string, err error) {
	owner, repo, ok := forge.SplitRepositoryPath(path)
	if !ok {
		return "", "", fmt.Errorf(
			"path %q does not contain a GitHub repository", path,
		)
	}

	return owner, repo, nil
}
