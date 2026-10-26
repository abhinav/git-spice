// Package github provides the typed GitHub API operations needed by git-spice.
//
// The package owns GitHub's GraphQL and REST wire protocols,
// authenticated request execution,
// and response error models.
// Callers supply credentials through [TokenSource] and adapt the typed results
// to their own domain models.
// The package does not own credential discovery, persistence, or login flows.
package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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
// configured GitHub endpoint.
// A typed operation may issue multiple requests when GitHub exposes related
// data through separate API nodes.
// GitHub responses containing both data and errors return only the errors;
// callers never observe a partial result.
type Gateway struct {
	// graphQLEndpoint is derived from the configured API base URL.
	graphQLEndpoint string

	// restBaseURL is the REST API root derived from the configured API base URL.
	restBaseURL *url.URL

	// httpClient performs requests after the gateway has supplied authentication.
	httpClient *http.Client

	// tokens supplies credentials at the lifetime of each operation.
	tokens TokenSource
}

// NewGateway builds a client for a GitHub API base URL.
//
// apiURLStr is the API base URL reported by the forge configuration.
// For GitHub Enterprise Server, this is the common `/api` root from which
// NewGateway derives `/api/graphql` and `/api/v3`.
// A nil HTTP client uses [http.DefaultClient].
// The tokens source is required and is consulted for every HTTP request.
func NewGateway(apiURLStr string, httpClient *http.Client, tokens TokenSource) (*Gateway, error) {
	apiURL, err := url.Parse(apiURLStr)
	if err != nil {
		return nil, fmt.Errorf("parse API URL: %w", err)
	}

	graphQLURL := apiURL.JoinPath("graphql")

	// TODO: Accept complete GraphQL and REST endpoint URLs instead of deriving
	// both endpoints from GitHub's common API base URL.
	// GitHub.com serves GraphQL below the REST API host,
	// while GitHub Enterprise Server uses /api/graphql and /api/v3.
	restBaseURL := apiURL
	if !strings.EqualFold(apiURL.Hostname(), "api.github.com") {
		restBaseURL = apiURL.JoinPath("v3")
	}

	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if tokens == nil {
		return nil, errors.New("token source is required")
	}

	return &Gateway{
		graphQLEndpoint: graphQLURL.String(),
		restBaseURL:     restBaseURL,
		httpClient:      httpClient,
		tokens:          tokens,
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
