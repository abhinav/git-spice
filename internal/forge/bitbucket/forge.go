package bitbucket

import (
	"cmp"
	"context"
	"fmt"
	"net/http"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
	"go.abhg.dev/gs/internal/git/giturl"
	"go.abhg.dev/gs/internal/silog"
)

// Definition configures Bitbucket forge instances.
type Definition struct {
	changeMetadataCodec

	// Options stores CLI and environment configuration.
	Options Options

	// Log specifies the logger to use.
	Log *silog.Logger
}

var (
	_ forge.Definition        = (*Definition)(nil)
	_ forge.Forge             = (*Forge)(nil)
	_ forge.WithCommentFormat = (*Forge)(nil)
)

// ID reports a unique key for this forge.
func (*Definition) ID() string { return "bitbucket" }

// BaseURL reports the Bitbucket web URL used for host matching.
func (d *Definition) BaseURL() string {
	return cmp.Or(d.Options.URL, DefaultURL)
}

// CLIPlugin returns the CLI plugin for the Bitbucket Forge.
func (d *Definition) CLIPlugin() any { return &d.Options }

// New constructs a Bitbucket Forge from the configured options.
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

// Forge provides a Bitbucket forge instance.
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
	return f.Log.WithPrefix("bitbucket")
}

// URL returns the base URL configured for the Bitbucket Forge
// or the default URL if none is set.
func (f *Forge) URL() string {
	return cmp.Or(f.Options.URL, DefaultURL)
}

// BaseURL reports the Bitbucket web URL used for host matching and links.
func (f *Forge) BaseURL() string {
	return f.baseURL
}

// APIURL returns the base API URL configured for the Bitbucket Forge
// or the default URL if none is set.
func (f *Forge) APIURL() string {
	return cmp.Or(f.Options.APIURL, DefaultAPIURL)
}

// ID reports a unique key for this forge.
func (*Forge) ID() string { return "bitbucket" }

// CommentFormat returns Bitbucket-specific comment formatting.
// Bitbucket doesn't support HTML in comments, so we use plain Markdown.
func (*Forge) CommentFormat() forge.CommentFormat {
	return forge.CommentFormat{
		// Use italic text instead of HTML <sub> tag.
		Footer: "*Change managed by [git-spice](https://abhinav.github.io/git-spice/).*",
		// Use Markdown link definition syntax instead of HTML comment.
		// This renders as invisible on Bitbucket.
		Marker: "[gs]: # (navigation comment)",
	}
}

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

// ParseRepositoryPath parses a Bitbucket repository path and returns
// a [RepositoryID] for the repository it identifies.
//
// It returns [ErrUnsupportedURL] if the path is not a valid Bitbucket path.
func (f *Forge) ParseRepositoryPath(path string) (forge.RepositoryID, error) {
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
	rid := mustRepositoryID(id)
	tok := token.(*AuthenticationToken)

	tokenSource, err := newGatewayTokenSource(tok)
	if err != nil {
		return nil, fmt.Errorf("build Bitbucket token source: %w", err)
	}

	client, err := bitbucket.NewClient(tokenSource, &bitbucket.ClientOptions{
		BaseURL: f.APIURL(),
	})
	if err != nil {
		return nil, fmt.Errorf("create Bitbucket client: %w", err)
	}

	return newRepository(f, rid.url, rid.workspace, rid.name, f.logger(), client, tok, http.DefaultClient), nil
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
