// Package gitlab provides a wrapper around GitLab's APIs
// in a manner compliant with the [forge.Forge] interface.
package gitlab

import (
	"cmp"
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"

	"go.abhg.dev/gs/internal/forge"
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

	return newRepository(ctx, f, rid.owner, rid.name, f.logger(), glc, nil)
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

	// We recognize the following GitLab remote URL formats:
	//
	//	http(s)://gitlab.com/OWNER/REPO.git
	//	git@gitlab.com:OWNER/REPO.git
	//
	// We can parse these all with url.Parse
	// if we normalize the latter to:
	//
	//	ssh://git@gitlab.com/OWNER/REPO.git
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
		return "", "", fmt.Errorf("%v is not a GitLab URL: expected host %q, got %q", u, baseURL.Host, u.Host)
	}

	s := u.Path                       // /OWNER/REPO.git/
	s = strings.TrimPrefix(s, "/")    // OWNER/REPO.git/
	s = strings.TrimSuffix(s, "/")    // OWNER/REPO/
	s = strings.TrimSuffix(s, ".git") // OWNER/REPO

	owner, repo, ok := strings.Cut(s, "/")
	if !ok {
		return "", "", fmt.Errorf("path %q does not contain a GitLab repository", s)
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
