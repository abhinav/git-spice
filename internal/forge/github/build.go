package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/google/go-github/v61/github"
	"golang.org/x/oauth2"
)

const (
	// DefaultBaseURL is the default URL for GitHub.
	DefaultBaseURL = "https://github.com"

	// DefaultAPIURL is the default URL for the GitHub API.
	DefaultAPIURL = "https://api.github.com"
)

// Builder builds a GitHub Forge.
type Builder struct {
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

// ErrUnsupportedURL is returned when the given URL is not a valid GitHub URL.
var ErrUnsupportedURL = errors.New("unsupported URL")

// New builds a new GitHub Forge from the given remote URL.
//
// Returns [ErrUnsupportedURL] if the URL is not a valid GitHub URL.
func (b *Builder) New(ctx context.Context, remoteURL string) (*Forge, error) {
	if b.URL == "" {
		b.URL = DefaultBaseURL
		// TODO: Use this to build API URL if not set.
	}
	if b.Log == nil {
		b.Log = log.New(io.Discard)
	}

	owner, repo, err := b.repoInfo(remoteURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnsupportedURL, err)
	}

	ghc := github.NewClient(oauth2.NewClient(ctx, b.Token))
	if b.APIURL != "" {
		var err error
		// FIXME: If we need to ever use the UploadURL,
		// we'll want to also fix that here.
		ghc, err = ghc.WithEnterpriseURLs(b.APIURL, ghc.UploadURL.String())
		if err != nil {
			return nil, fmt.Errorf("set GitHub API URL: %w", err)
		}
	}

	return &Forge{
		owner:  owner,
		repo:   repo,
		log:    b.Log,
		client: ghc,
	}, nil
}

func (b *Builder) repoInfo(remoteURL string) (owner, repo string, err error) {
	baseURL, err := url.Parse(b.URL)
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
