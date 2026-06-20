package bitbucket

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/url"

	"go.abhg.dev/gs/internal/forge"
	gw "go.abhg.dev/gs/internal/gateway/bitbucket"
	"go.abhg.dev/gs/internal/gateway/bitbucket/cloud"
	"go.abhg.dev/gs/internal/gateway/bitbucket/server"
	"go.abhg.dev/gs/internal/silog"
)

// Forge builds a Bitbucket Forge.
type Forge struct {
	Options Options

	// Log specifies the logger to use.
	Log *silog.Logger
}

var (
	_ forge.Forge               = (*Forge)(nil)
	_ forge.WithCommentFormat   = (*Forge)(nil)
	_ forge.RemoteURLConfigurer = (*Forge)(nil)
)

func (f *Forge) logger() *silog.Logger {
	if f.Log == nil {
		return silog.Nop()
	}
	return f.Log.WithPrefix("bitbucket")
}

// kind returns the selected Bitbucket product.
func (f *Forge) kind() Kind {
	if f.Options.Kind != KindAuto {
		return f.Options.Kind
	}

	if f.Options.URL == "" {
		return KindCloud
	}

	if u, err := url.Parse(f.Options.URL); err == nil && isCloudHost(u.Hostname()) {
		return KindCloud
	}
	return KindDataCenter
}

// URL returns the base URL configured for the Bitbucket Forge
// or the default URL if none is set.
func (f *Forge) URL() string {
	return cmp.Or(f.Options.URL, DefaultURL)
}

// BaseURL reports the Bitbucket web URL used for host matching and links.
func (f *Forge) BaseURL() string {
	return f.URL()
}

// APIURL returns the configured API URL or the product default.
func (f *Forge) APIURL() string {
	if f.kind() == KindDataCenter {
		return cmp.Or(f.Options.APIURL, f.URL()+"/rest/api/1.0")
	}
	return cmp.Or(f.Options.APIURL, DefaultAPIURL)
}

// ID reports a unique key for this forge.
func (*Forge) ID() string { return "bitbucket" }

const _navigationCommentMarker = "[gs]: # (navigation comment)"

// CommentFormat returns Bitbucket-specific comment formatting.
// Bitbucket doesn't support HTML in comments, so we use plain Markdown.
func (*Forge) CommentFormat() forge.CommentFormat {
	return forge.CommentFormat{
		// Use italic text instead of HTML <sub> tag.
		Footer: "*Change managed by [git-spice](https://abhinav.github.io/git-spice/).*",
		// Use Markdown link definition syntax instead of HTML comment.
		// This renders as invisible on Bitbucket.
		Marker: _navigationCommentMarker,
	}
}

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

// ParseRepositoryPath parses a Bitbucket repository path.
func (f *Forge) ParseRepositoryPath(path string) (forge.RepositoryID, error) {
	if f.kind() == KindDataCenter {
		projectKey, slug, personal, err := parseServerRepoPath(path)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", forge.ErrUnsupportedURL, err)
		}

		return &serverRepositoryID{
			url:        f.URL(),
			projectKey: projectKey,
			slug:       slug,
			personal:   personal,
		}, nil
	}

	workspace, repo, err := extractRepoInfo(path)
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
	tok := token.(*AuthenticationToken)
	log := f.logger()

	var (
		gateway gw.Gateway
		err     error
	)
	if f.kind() == KindDataCenter {
		rid := mustServerRepositoryID(id)
		var stok *server.Token
		if tok != nil {
			stok = &server.Token{AccessToken: tok.AccessToken}
		}
		gateway, err = server.New(
			f.APIURL(), f.Options.URL,
			rid.projectKey, rid.slug, rid.personal,
			log, stok,
		)
	} else {
		rid := mustRepositoryID(id)
		var ctok *cloud.Token
		if tok != nil {
			ctok = &cloud.Token{AccessToken: tok.AccessToken}
		}
		gateway, err = cloud.New(
			f.APIURL(), f.URL(),
			rid.workspace, rid.name,
			log, ctok, http.DefaultClient,
		)
	}
	if err != nil {
		return nil, err
	}

	return newRepository(f, log, gateway), nil
}

func extractRepoInfo(path string) (workspace, repo string, err error) {
	workspace, repo, ok := forge.SplitRepositoryPath(path)
	if !ok {
		return "", "", fmt.Errorf(
			"path %q does not contain a Bitbucket repository", path,
		)
	}

	return workspace, repo, nil
}
