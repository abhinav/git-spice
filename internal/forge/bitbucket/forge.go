package bitbucket

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
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
		switch baseURL {
		case "":
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
	log := d.Log
	if log == nil {
		log = silog.Nop()
	} else {
		log = log.WithPrefix("bitbucket")
	}

	var product bitbucketProduct
	switch kind {
	case KindDataCenter:
		if baseURL == "" {
			return nil, errNoServerURL
		}
		if apiURL == "" {
			apiURL = baseURL + "/rest/api/1.0"
		}
		product = bitbucketDataCenterProduct{
			baseURL: baseURL,
			apiURL:  apiURL,
			log:     log,
		}
	case KindCloud:
		baseURL = cmp.Or(baseURL, DefaultURL)
		apiURL = cmp.Or(apiURL, DefaultAPIURL)
		product = bitbucketCloudProduct{
			baseURL: baseURL,
			apiURL:  apiURL,
			log:     log,
		}
	default:
		return nil, fmt.Errorf("invalid Bitbucket product: %s", kind)
	}

	return &Forge{
		baseURL: baseURL,
		apiURL:  apiURL,
		kind:    kind,
		token:   options.Token,
		product: product,
		Log:     d.Log,
	}, nil
}

// Forge provides a Bitbucket forge instance.
type Forge struct {
	changeMetadataCodec

	baseURL string
	apiURL  string
	kind    Kind
	token   string
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

// URL returns the resolved Bitbucket web URL.
func (f *Forge) URL() string {
	return f.baseURL
}

// BaseURL reports the Bitbucket web URL used for host matching and links.
func (f *Forge) BaseURL() string {
	return f.baseURL
}

// APIURL returns the resolved Bitbucket API URL.
func (f *Forge) APIURL() string {
	return f.apiURL
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
	return f.product.parseRepositoryPath(path)
}

// OpenRepository opens the Bitbucket repository that the given ID points to.
func (f *Forge) OpenRepository(
	ctx context.Context,
	token forge.AuthenticationToken,
	id forge.RepositoryID,
) (forge.Repository, error) {
	tok := token.(*AuthenticationToken)
	gateway, err := f.product.openGateway(ctx, tok, mustRepositoryID(id))
	if err != nil {
		return nil, err
	}
	return newRepository(f, f.logger(), gateway), nil
}

// bitbucketProduct fixes the product-specific repository behavior
// selected during Definition.New.
type bitbucketProduct interface {
	parseRepositoryPath(path string) (*RepositoryID, error)
	openGateway(context.Context, *AuthenticationToken, *RepositoryID) (bitbucket.Gateway, error)
}

type bitbucketCloudProduct struct {
	baseURL string
	apiURL  string
	log     *silog.Logger
}

func (p bitbucketCloudProduct) parseRepositoryPath(path string) (*RepositoryID, error) {
	workspace, repo, err := extractRepoInfo(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", forge.ErrUnsupportedURL, err)
	}

	return &RepositoryID{
		url:       p.baseURL,
		kind:      KindCloud,
		workspace: workspace,
		name:      repo,
	}, nil
}

func (p bitbucketCloudProduct) openGateway(
	_ context.Context,
	tok *AuthenticationToken,
	rid *RepositoryID,
) (bitbucket.Gateway, error) {
	var ctok *cloud.Token
	if tok != nil {
		ctok = &cloud.Token{AccessToken: tok.AccessToken}
	}

	return cloud.New(
		p.apiURL, p.baseURL,
		rid.workspace, rid.name,
		p.log, ctok, http.DefaultClient,
	)
}

type bitbucketDataCenterProduct struct {
	baseURL string
	apiURL  string
	log     *silog.Logger
}

func (p bitbucketDataCenterProduct) parseRepositoryPath(path string) (*RepositoryID, error) {
	errInvalid := fmt.Errorf(
		"path %q does not contain a Bitbucket Data Center repository", path,
	)

	s := strings.Trim(path, "/")
	s = strings.TrimSuffix(s, ".git")

	segments := strings.Split(s, "/")
	if i := slices.Index(segments, "scm"); i >= 0 {
		segments = segments[i+1:]
	}

	if len(segments) != 2 || segments[0] == "" || segments[1] == "" {
		return nil, fmt.Errorf("%w: %w", forge.ErrUnsupportedURL, errInvalid)
	}

	projectKey, slug := segments[0], segments[1]
	personal := false
	if user, ok := strings.CutPrefix(projectKey, "~"); ok {
		if user == "" {
			return nil, fmt.Errorf("%w: %w", forge.ErrUnsupportedURL, errInvalid)
		}
		projectKey = user
		personal = true
	}

	return &RepositoryID{
		url:        p.baseURL,
		kind:       KindDataCenter,
		projectKey: projectKey,
		slug:       slug,
		personal:   personal,
	}, nil
}

func (p bitbucketDataCenterProduct) openGateway(
	_ context.Context,
	tok *AuthenticationToken,
	rid *RepositoryID,
) (bitbucket.Gateway, error) {
	var stok *server.Token
	if tok != nil {
		stok = &server.Token{AccessToken: tok.AccessToken}
	}

	return server.New(
		p.apiURL, rid.url,
		rid.projectKey, rid.slug, rid.personal,
		p.log, stok,
	)
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
