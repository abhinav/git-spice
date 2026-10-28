// Package github provides the typed GitHub API operations needed by git-spice.
//
// The package owns GitHub's deployment URL conventions,
// GraphQL and REST wire protocols,
// authenticated request execution,
// and response error models.
// Callers supply a [Deployment] through [Config] and credentials through
// [TokenSource], and adapt the typed results to their own domain models.
// The package does not own credential discovery, persistence, or login flows.
package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// maxErrorBody limits the diagnostic content retained from an HTTP error
// response so a proxy or server cannot turn an error into an unbounded
// allocation or diagnostic.
const maxErrorBody = 4 * 1024

// TokenSource supplies an access token for one GitHub request.
type TokenSource interface {
	// Token returns the access token for the request associated with ctx.
	Token(context.Context) (string, error)
}

// Gateway executes the typed GitHub operations exposed by this package.
//
// A Gateway is safe for concurrent use when its HTTP client and token source are
// safe for concurrent use.
// Each HTTP request retrieves a token with the operation context and uses the
// configured GitHub endpoints.
// A typed operation may issue multiple requests when GitHub exposes related
// data through separate API nodes.
// GitHub responses containing both data and errors return only the errors;
// callers never observe a partial result.
type Gateway struct {
	// graphQLEndpoint is the complete URL for GraphQL requests.
	graphQLEndpoint *url.URL

	// restBaseURL is the root to which REST resource paths are appended.
	restBaseURL *url.URL

	// httpClient performs requests after the gateway has supplied authentication.
	httpClient *http.Client

	// tokens supplies credentials at the lifetime of each operation.
	tokens TokenSource
}

// Config configures a Gateway's deployment and authenticated HTTP requests.
type Config struct {
	// Deployment selects the GitHub installation.
	// Its zero value selects GitHub.com.
	Deployment Deployment

	// Tokens supplies credentials for every HTTP request.
	Tokens TokenSource // required

	// HTTPClient sends requests to both endpoints.
	// If nil, [http.DefaultClient] is used.
	HTTPClient *http.Client
}

// NewGateway resolves the deployment's API addresses and builds a client.
func NewGateway(cfg Config) (*Gateway, error) {
	endpoints, err := cfg.Deployment.apiEndpoints()
	if err != nil {
		return nil, err
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if cfg.Tokens == nil {
		return nil, errors.New("token source is required")
	}

	return &Gateway{
		graphQLEndpoint: endpoints.GraphQLEndpoint,
		restBaseURL:     endpoints.RESTBaseURL,
		httpClient:      httpClient,
		tokens:          cfg.Tokens,
	}, nil
}

// newRequest builds one authenticated GitHub request.
// Token lookup stays at request lifetime rather than operation lifetime because
// one typed operation may issue multiple HTTP requests.
func (c *Gateway) newRequest(
	ctx context.Context,
	method string,
	endpoint string,
	body io.Reader,
) (*http.Request, error) {
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("get GitHub token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("build GitHub request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return req, nil
}
