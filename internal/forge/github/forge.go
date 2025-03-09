// Package github provides a wrapper around GitHub's APIs
// in a manner compliant with the [forge.Forge] interface.
package github

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
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
	Log *log.Logger
}

var _ forge.Forge = (*Forge)(nil)

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

// MatchURL reports whether the given URL is a GitHub URL.
func (f *Forge) MatchURL(remoteURL string) bool {
	_, _, err := extractRepoInfo(f.URL(), remoteURL)
	return err == nil
}

// ChangeURL returns a URL to view a change on GitHub.
// Returns an empty string if the URL is not a valid GitHub URL.
func (f *Forge) ChangeURL(remoteURL string, id forge.ChangeID) (string, error) {
	owner, repo, err := extractRepoInfo(f.URL(), remoteURL)
	if err != nil {
		return "", fmt.Errorf("%w: %w", forge.ErrUnsupportedURL, err)
	}

	pr := mustPR(id)
	return fmt.Sprintf("%s/%s/%s/pull/%d", f.URL(), owner, repo, pr.Number), nil
}

// OpenURL opens a GitHub repository from a remote URL.
// Returns [forge.ErrUnsupportedURL] if the URL is not a valid GitHub URL.
func (f *Forge) OpenURL(ctx context.Context, tok forge.AuthenticationToken, remoteURL string) (forge.Repository, error) {
	if f.Log == nil {
		f.Log = log.New(io.Discard)
	}

	owner, repo, err := extractRepoInfo(f.URL(), remoteURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", forge.ErrUnsupportedURL, err)
	}

	tokenSource := tok.(*AuthenticationToken).tokenSource()
	ghc, err := newGitHubv4Client(ctx, f.APIURL(), tokenSource)
	if err != nil {
		return nil, fmt.Errorf("create GitHub client: %w", err)
	}

	return newRepository(ctx, f, owner, repo, f.Log, ghc, nil)
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

	// We recognize the following GitHub remote URL formats:
	//
	//	http(s)://github.com/OWNER/REPO.git
	//	git@github.com:OWNER/REPO.git
	//
	// We can parse these all with url.Parse
	// if we normalize the latter to:
	//
	//	ssh://git@github.com/OWNER/REPO.git
	if !hasGitProtocol(remoteURL) && strings.Contains(remoteURL, ":") {
		// $user@$host:$path => ssh://$user@$host/$path
		remoteURL = "ssh://" + strings.Replace(remoteURL, ":", "/", 1)
	}

	u, err := url.Parse(remoteURL)
	if err != nil {
		return "", "", fmt.Errorf("parse remote URL: %w", err)
	}

	// If base URL doesn't explicitly specify a port,
	// and the remote URL does, *and* it's a default port,
	// strip it from the remote URL.
	if baseURL.Port() == "" {
		if host, port, err := net.SplitHostPort(u.Host); err == nil {
			switch port {
			case "443", "80":
				u.Host = host
			}
		}
	}

	// May be a subdomain of base URL.
	if u.Host != baseURL.Host && !strings.HasSuffix(u.Host, "."+baseURL.Host) {
		return "", "", fmt.Errorf("%v is not a GitHub URL: expected host %q, got %q", u, baseURL.Host, u.Host)
	}

	s := u.Path                       // /OWNER/REPO.git/
	s = strings.TrimPrefix(s, "/")    // OWNER/REPO.git/
	s = strings.TrimSuffix(s, "/")    // OWNER/REPO/
	s = strings.TrimSuffix(s, ".git") // OWNER/REPO

	owner, repo, ok := strings.Cut(s, "/")
	if !ok {
		return "", "", fmt.Errorf("path %q does not contain a GitHub repository", s)
	}

	return owner, repo, nil
}

// _gitProtocols is a list of known git protocols
// including the :// suffix.
var _gitProtocols = []string{
	"ssh",
	"git",
	"git+ssh",
	"git+https",
	"git+http",
	"https",
	"http",
}

func init() {
	for i, proto := range _gitProtocols {
		_gitProtocols[i] = proto + "://"
	}
}

func hasGitProtocol(url string) bool {
	for _, proto := range _gitProtocols {
		if strings.HasPrefix(url, proto) {
			return true
		}
	}
	return false
}
