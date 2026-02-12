package bitbucket

import (
	"cmp"
	"context"
	"fmt"
	"net/url"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgeurl"
	"go.abhg.dev/gs/internal/silog"
)

// Forge builds a Bitbucket Forge.
type Forge struct {
	Options Options

	// Log specifies the logger to use.
	Log *silog.Logger
}

var (
	_ forge.Forge           = (*Forge)(nil)
	_ forge.WithDisplayName = (*Forge)(nil)
)

func (f *Forge) logger() *silog.Logger {
	if f.Log == nil {
		return silog.Nop()
	}
	return f.Log.WithPrefix("bitbucket")
}

// URL returns the base URL configured for the Bitbucket Forge
// or the default URL if none is set.
func (f *Forge) URL() string {
	return cmp.Or(f.Options.URL, DefaultURL)
}

// APIURL returns the base API URL configured for the Bitbucket Forge
// or the default URL if none is set.
func (f *Forge) APIURL() string {
	return cmp.Or(f.Options.APIURL, DefaultAPIURL)
}

// ID reports a unique key for this forge.
func (*Forge) ID() string { return "bitbucket" }

// DisplayName returns a human-friendly name for the forge.
func (*Forge) DisplayName() string { return "Bitbucket (Atlassian)" }

// CLIPlugin returns the CLI plugin for the Bitbucket Forge.
func (f *Forge) CLIPlugin() any { return &f.Options }

// ChangeTemplatePaths reports the paths at which change templates
// can be found in a Bitbucket repository.
func (*Forge) ChangeTemplatePaths() []string {
	// Bitbucket does not have native PR template support like GitHub/GitLab.
	// Some repositories use community conventions.
	return []string{
		"PULL_REQUEST_TEMPLATE.md",
		"pull_request_template.md",
		".bitbucket/PULL_REQUEST_TEMPLATE.md",
		".bitbucket/pull_request_template.md",
	}
}

// ParseRemoteURL parses the given remote URL and returns a [RepositoryID]
// for the Bitbucket repository it points to.
//
// It returns [ErrUnsupportedURL] if the remote URL is not a valid Bitbucket URL.
func (f *Forge) ParseRemoteURL(remoteURL string) (forge.RepositoryID, error) {
	workspace, repo, err := extractRepoInfo(f.URL(), remoteURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", forge.ErrUnsupportedURL, err)
	}

	return &RepositoryID{
		url:       f.URL(),
		workspace: workspace,
		name:      repo,
	}, nil
}

// OpenRepository opens the Bitbucket repository that the given ID points to.
func (f *Forge) OpenRepository(
	_ context.Context,
	token forge.AuthenticationToken,
	id forge.RepositoryID,
) (forge.Repository, error) {
	rid := mustRepositoryID(id)
	tok := token.(*AuthenticationToken)

	client := newClient(f.APIURL(), tok, f.logger())
	return newRepository(f, rid.workspace, rid.name, f.logger(), client), nil
}

func extractRepoInfo(bitbucketURL, remoteURL string) (workspace, repo string, err error) {
	baseURL, err := url.Parse(bitbucketURL)
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
			"%v is not a Bitbucket URL: expected host %q, got %q",
			u, baseURL.Host, u.Host,
		)
	}

	workspace, repo, ok := forgeurl.ExtractPath(u.Path)
	if !ok {
		return "", "", fmt.Errorf(
			"path %q does not contain a Bitbucket repository", u.Path,
		)
	}

	return workspace, repo, nil
}
