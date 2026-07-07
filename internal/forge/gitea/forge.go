// Package gitea provides a wrapper around Gitea's APIs
// in a manner compliant with the [forge.Forge] interface.
package gitea

import (
	"cmp"
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
	"go.abhg.dev/gs/internal/git/giturl"
	"go.abhg.dev/gs/internal/silog"
)

// Options defines command line options for the Gitea Forge.
// These are all hidden in the CLI,
// and are expected to be set only via environment variables.
type Options struct {
	// URL is the base URL of the Gitea instance.
	// Required for Gitea since there is no canonical cloud host.
	URL string `name:"gitea-url" hidden:"" config:"forge.gitea.url" env:"GITEA_URL" help:"Base URL for Gitea instance"`

	// APIURL is the URL for Gitea's API.
	// Override this for testing or instances with non-standard API paths.
	APIURL string `name:"gitea-api-url" hidden:"" config:"forge.gitea.apiURL" env:"GITEA_API_URL" help:"Base URL for Gitea API requests"`

	// Token is a fixed token used to authenticate with Gitea.
	// This may be used to skip the login flow.
	Token string `name:"gitea-token" hidden:"" env:"GITEA_TOKEN" help:"Gitea API token"`
}

// Definition configures Gitea forge instances.
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
func (*Definition) ID() string { return "gitea" }

// BaseURL reports the Gitea web URL used for host matching.
func (d *Definition) BaseURL() string {
	return d.Options.URL
}

// CLIPlugin returns the CLI plugin for the Gitea Forge.
func (d *Definition) CLIPlugin() any { return &d.Options }

// New constructs a Gitea Forge from the configured options.
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

// Forge provides a Gitea forge instance.
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
	return f.Log.WithPrefix("gitea")
}

// URL returns the base URL configured for the Gitea Forge.
func (f *Forge) URL() string {
	return f.Options.URL
}

// BaseURL reports the Gitea instance URL used for host matching and links.
// Returns an empty string if not configured.
func (f *Forge) BaseURL() string {
	return f.baseURL
}

// apiURL returns the API URL to use for Gitea requests.
func (f *Forge) apiURL() string {
	return cmp.Or(f.Options.APIURL, f.URL())
}

// ID reports a unique key for this forge.
func (*Forge) ID() string { return "gitea" }

// ParseRepositoryPath parses a Gitea repository path and returns a [RepositoryID]
// for the Gitea repository it identifies.
func (f *Forge) ParseRepositoryPath(path string) (forge.RepositoryID, error) {
	if f.URL() == "" {
		return nil, fmt.Errorf("%w: Gitea URL is not configured (set GITEA_URL)",
			forge.ErrUnsupportedURL)
	}

	owner, repo, ok := forge.SplitRepositoryPath(path)
	if !ok {
		return nil, fmt.Errorf("%w: path %q does not contain a Gitea repository",
			forge.ErrUnsupportedURL, path)
	}

	return &RepositoryID{
		url:   f.URL(),
		owner: owner,
		name:  repo,
	}, nil
}

// OpenRepository opens the Gitea repository that the given ID points to.
func (f *Forge) OpenRepository(ctx context.Context, token forge.AuthenticationToken, id forge.RepositoryID) (forge.Repository, error) {
	rid := mustRepositoryID(id)

	tokenSource, err := newGatewayTokenSource(token.(*AuthenticationToken))
	if err != nil {
		return nil, fmt.Errorf("build Gitea token source: %w", err)
	}

	gc, err := giteagw.NewClient(tokenSource, &giteagw.ClientOptions{
		BaseURL: f.apiURL(),
	})
	if err != nil {
		return nil, fmt.Errorf("create Gitea client: %w", err)
	}

	return newRepository(ctx, f, rid.owner, rid.name, f.logger(), gc)
}

// ChangeTemplatePaths reports the case-insensitive paths at which
// it's possible to define PR templates in the repository.
func (f *Forge) ChangeTemplatePaths() []string {
	return []string{
		".gitea/PULL_REQUEST_TEMPLATE.md",
		".gitea/pull_request_template.md",
		".github/PULL_REQUEST_TEMPLATE.md",
		".github/pull_request_template.md",
		"PULL_REQUEST_TEMPLATE.md",
		"pull_request_template.md",
	}
}

// RepositoryID is a unique identifier for a Gitea repository.
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
	panic(fmt.Sprintf("gitea: expected *RepositoryID, got %T", id))
}

// String returns a human-readable name for the repository ID.
func (rid *RepositoryID) String() string {
	return rid.owner + "/" + rid.name
}

// ChangeURL returns the web URL for a pull request hosted on Gitea.
func (rid *RepositoryID) ChangeURL(id forge.ChangeID) string {
	return fmt.Sprintf("%s/%s/%s/pulls/%d",
		rid.url, rid.owner, rid.name, mustPR(id).Number)
}
