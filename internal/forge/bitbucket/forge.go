package bitbucket

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket/cloud"
	"go.abhg.dev/gs/internal/gateway/bitbucket/server"
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

	options := d.Options
	baseURL := options.URL
	if baseURL == "" && options.Kind != KindCloud &&
		remoteURL.Hostname != "" && !isCloudHost(remoteURL.Hostname) {
		baseURL = deriveInstanceURL(remoteURL)
	}

	kind := options.Kind
	if kind == KindAuto {
		switch {
		case baseURL == "":
			kind = KindCloud
		default:
			u, err := url.Parse(baseURL)
			if err == nil && isCloudHost(u.Hostname()) {
				kind = KindCloud
			} else {
				kind = KindDataCenter
			}
		}
	}

	apiURL := options.APIURL
	var product bitbucketProduct
	switch kind {
	case KindDataCenter:
		if baseURL == "" {
			return nil, errNoServerURL
		}
		if apiURL == "" {
			apiURL = baseURL + "/rest/api/1.0"
		}
		product = dataCenterProduct{}
	case KindCloud:
		baseURL = cmp.Or(baseURL, DefaultURL)
		apiURL = cmp.Or(apiURL, DefaultAPIURL)
		product = cloudProduct{}
	default:
		return nil, fmt.Errorf("invalid Bitbucket product: %s", kind)
	}

	return &Forge{
		Options: d.Options,
		baseURL: baseURL,
		apiURL:  apiURL,
		kind:    kind,
		product: product,
		Log:     d.Log,
	}, nil
}

// Forge provides a Bitbucket forge instance.
type Forge struct {
	changeMetadataCodec

	Options Options
	baseURL string
	apiURL  string
	kind    Kind
	product bitbucketProduct

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
	return cmp.Or(f.baseURL, f.Options.URL, DefaultURL)
}

// BaseURL reports the Bitbucket web URL used for host matching and links.
func (f *Forge) BaseURL() string {
	return f.URL()
}

// APIURL returns the configured API URL or the product default.
func (f *Forge) APIURL() string {
	return cmp.Or(f.apiURL, f.Options.APIURL, DefaultAPIURL)
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

// ParseRepositoryPath parses a Bitbucket repository path.
func (f *Forge) ParseRepositoryPath(path string) (forge.RepositoryID, error) {
	if f.product == nil {
		return nil, fmt.Errorf("%w: Bitbucket forge was not constructed by Definition.New", forge.ErrUnsupportedURL)
	}
	return f.product.parseRepositoryPath(f.URL(), path)
}

// OpenRepository opens the Bitbucket repository that the given ID points to.
func (f *Forge) OpenRepository(
	ctx context.Context,
	token forge.AuthenticationToken,
	id forge.RepositoryID,
) (forge.Repository, error) {
	tok := token.(*AuthenticationToken)
	if f.product == nil {
		return nil, errors.New("Bitbucket forge was not constructed by Definition.New")
	}
	return f.product.openRepository(ctx, f, tok, mustRepositoryID(id))
}

type bitbucketProduct interface {
	parseRepositoryPath(baseURL, path string) (*RepositoryID, error)
	openRepository(context.Context, *Forge, *AuthenticationToken, *RepositoryID) (forge.Repository, error)
}

// cloudProduct fixes Cloud-specific repository behavior for a constructed Forge.
type cloudProduct struct{}

func (cloudProduct) parseRepositoryPath(baseURL, path string) (*RepositoryID, error) {
	workspace, repo, err := extractRepoInfo(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", forge.ErrUnsupportedURL, err)
	}

	return &RepositoryID{
		url:       baseURL,
		kind:      KindCloud,
		workspace: workspace,
		name:      repo,
	}, nil
}

func (cloudProduct) openRepository(
	_ context.Context,
	f *Forge,
	tok *AuthenticationToken,
	rid *RepositoryID,
) (forge.Repository, error) {
	var ctok *cloud.Token
	if tok != nil {
		ctok = &cloud.Token{AccessToken: tok.AccessToken}
	}

	gateway, err := cloud.New(
		f.APIURL(), f.URL(),
		rid.workspace, rid.name,
		f.logger(), ctok, http.DefaultClient,
	)
	if err != nil {
		return nil, err
	}

	return newRepository(f, f.logger(), gateway), nil
}

// dataCenterProduct fixes Data Center repository behavior for a constructed Forge.
type dataCenterProduct struct{}

func (dataCenterProduct) parseRepositoryPath(baseURL, path string) (*RepositoryID, error) {
	projectKey, slug, personal, err := parseServerRepoPath(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", forge.ErrUnsupportedURL, err)
	}

	return &RepositoryID{
		url:        baseURL,
		kind:       KindDataCenter,
		projectKey: projectKey,
		slug:       slug,
		personal:   personal,
	}, nil
}

func (dataCenterProduct) openRepository(
	_ context.Context,
	f *Forge,
	tok *AuthenticationToken,
	rid *RepositoryID,
) (forge.Repository, error) {
	var stok *server.Token
	if tok != nil {
		stok = &server.Token{AccessToken: tok.AccessToken}
	}

	gateway, err := server.New(
		f.APIURL(), rid.url,
		rid.projectKey, rid.slug, rid.personal,
		f.logger(), stok,
	)
	if err != nil {
		return nil, err
	}

	return newRepository(f, f.logger(), gateway), nil
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
