// Package github provides a wrapper around GitHub's APIs
// in a manner compliant with the [forge.Forge] interface.
package github

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
	"golang.org/x/oauth2"
)

const (
	// DefaultBaseURL is the default URL for GitHub.
	DefaultBaseURL = "https://github.com"

	// DefaultAPIURL is the default URL for the GitHub API.
	DefaultAPIURL = "https://api.github.com"
)

// Forge builds a GitHub Forge.
type Forge struct {
	// URL is the URL for GitHub.
	// Override this for testing or GitHub Enterprise.
	URL string

	// APIURL is the URL for the GitHub API.
	// Override this for testing or GitHub Enterprise.
	APIURL string

	// Token is the OAuth2 token source to use
	// to authenticate with GitHub.
	Token oauth2.TokenSource

	// Log specifies the logger to use.
	Log *log.Logger
}

var _ forge.Forge = (*Forge)(nil)

// ID reports a unique key for this forge.
func (*Forge) ID() string { return "github" }

// OpenURL opens a GitHub repository from a remote URL.
// Returns [forge.ErrUnsupportedURL] if the URL is not a valid GitHub URL.
func (f *Forge) OpenURL(ctx context.Context, remoteURL string) (forge.Repository, error) {
	if f.URL == "" {
		f.URL = DefaultBaseURL
		// TODO: Use this to build API URL if not set.
	}
	if f.Log == nil {
		f.Log = log.New(io.Discard)
	}

	owner, repo, err := f.repoInfo(remoteURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", forge.ErrUnsupportedURL, err)
	}

	oauthClient := oauth2.NewClient(ctx, f.Token)
	var ghc *githubv4.Client
	if f.APIURL != "" {
		ghc = githubv4.NewEnterpriseClient(f.APIURL, oauthClient)
	} else {
		ghc = githubv4.NewClient(oauthClient)
	}

	return newRepository(ctx, owner, repo, f.Log, ghc)
}

func (f *Forge) repoInfo(remoteURL string) (owner, repo string, err error) {
	baseURL, err := url.Parse(f.URL)
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

	if u.Host != baseURL.Host {
		return "", "", fmt.Errorf("%v is not a GitHub URL: expected host %q", u, baseURL.Host)
	}

	s := u.Path                       // /OWNER/REPO.git
	s = strings.TrimPrefix(s, "/")    // OWNER/REPO.git
	s = strings.TrimSuffix(s, ".git") // OWNER/REPO

	owner, repo, ok := strings.Cut(s, "/")
	if !ok {
		return "", "", fmt.Errorf("path %q does not contain a GitHub repo", s)
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
