// Package forgejo provides a wrapper around Forgejo's APIs
// in a manner compliant with the [forge.Forge] interface.
package forgejo

import (
	"cmp"
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/forgejo"
	"go.abhg.dev/gs/internal/git/giturl"
	"go.abhg.dev/gs/internal/silog"
)

const (
	// DefaultURL is the default URL for Forgejo.
	DefaultURL = "https://codeberg.org"
)

// Options defines command line options for the Forgejo Forge.
// These are hidden from CLI help,
// but URL settings may also be supplied by configuration.
type Options struct {
	// URL is the URL for Forgejo.
	// Override this for testing or self-hosted Forgejo instances.
	URL string `name:"forgejo-url" hidden:"" config:"forge.forgejo.url" env:"FORGEJO_URL" help:"Base URL for Forgejo web requests"`

	// APIURL is the URL for Forgejo's API.
	// Override this for testing or self-hosted Forgejo instances.
	APIURL string `name:"forgejo-api-url" hidden:"" config:"forge.forgejo.apiURL" env:"FORGEJO_API_URL" help:"Base URL for Forgejo API requests"`

	// Token is a fixed token used to authenticate with Forgejo.
	// This may be used to skip the login flow.
	Token string `name:"forgejo-token" hidden:"" env:"FORGEJO_TOKEN" help:"Forgejo API token"`
}

// Definition configures Forgejo forge instances.
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
func (*Definition) ID() string { return "forgejo" }

// BaseURL reports the Forgejo web URL used for host matching.
func (d *Definition) BaseURL() string {
	return cmp.Or(d.Options.URL, DefaultURL)
}

// CLIPlugin returns the CLI plugin for the Forgejo Forge.
func (d *Definition) CLIPlugin() any { return &d.Options }

// New constructs a Forgejo Forge from the configured options.
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

// Forge provides a Forgejo forge instance.
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
	return f.Log.WithPrefix("forgejo")
}

// URL returns the base URL configured for the Forgejo Forge
// or the default URL if none is set.
func (f *Forge) URL() string {
	return cmp.Or(f.Options.URL, DefaultURL)
}

// BaseURL reports the Forgejo web URL used for host matching and links.
func (f *Forge) BaseURL() string {
	return f.baseURL
}

// APIURL returns the base API URL configured for the Forgejo Forge
// or the default URL if none is set.
func (f *Forge) APIURL() string {
	return cmp.Or(f.Options.APIURL, f.URL())
}

// ID reports a unique key for this forge.
func (*Forge) ID() string { return "forgejo" }

// ParseRepositoryPath parses a Forgejo repository path and returns
// a [forge.RepositoryID] for the repository it identifies.
//
// It returns [forge.ErrUnsupportedURL] if the path is not a valid
// Forgejo repository path.
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

// OpenRepository opens the Forgejo repository that the given ID points to.
func (f *Forge) OpenRepository(
	ctx context.Context,
	token forge.AuthenticationToken,
	id forge.RepositoryID,
) (forge.Repository, error) {
	rid := mustRepositoryID(id)

	tokenSource, err := newGatewayTokenSource(token.(*AuthenticationToken))
	if err != nil {
		return nil, fmt.Errorf("build Forgejo token source: %w", err)
	}

	client, err := forgejo.NewClient(tokenSource, &forgejo.ClientOptions{
		BaseURL: f.APIURL(),
	})
	if err != nil {
		return nil, fmt.Errorf("create Forgejo client: %w", err)
	}

	return NewRepository(ctx, f, rid.owner, rid.name, f.logger(), client)
}

// RepositoryID is a unique identifier for a Forgejo repository.
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
	panic(fmt.Sprintf("forgejo: expected *RepositoryID, got %T", id))
}

// String returns a human-readable name for the repository ID.
func (rid *RepositoryID) String() string {
	return rid.owner + "/" + rid.name
}

// ChangeURL returns the URL for a Change hosted on Forgejo.
func (rid *RepositoryID) ChangeURL(id forge.ChangeID) string {
	return fmt.Sprintf(
		"%s/%s/%s/pulls/%d",
		rid.url,
		rid.owner,
		rid.name,
		mustPR(id).Number,
	)
}

func extractRepoInfo(path string) (owner, repo string, err error) {
	owner, repo, ok := forge.SplitRepositoryPath(path)
	if !ok {
		return "", "", fmt.Errorf(
			"path %q does not contain a Forgejo repository", path,
		)
	}

	return owner, repo, nil
}
