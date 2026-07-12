// Package github provides the GitHub GraphQL operations needed by git-spice.
//
// The package owns GitHub's GraphQL wire protocol, authenticated request
// execution, and response error model.
// Callers supply credentials through [TokenSource] and adapt the typed results
// to their own domain models.
// The package does not own credential discovery, persistence, or login flows.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// maxErrorBody limits the diagnostic content retained from a non-GraphQL HTTP
// response so a proxy or server cannot turn an error into an unbounded
// allocation or diagnostic.
const maxErrorBody = 4 * 1024

// TokenSource supplies an access token for one GitHub request.
type TokenSource interface {
	// Token returns the access token for the request associated with ctx.
	Token(context.Context) (string, error)
}

// Gateway executes the typed GitHub GraphQL operations exposed by this package.
//
// A Gateway is safe for concurrent use when its HTTP client and token source are
// safe for concurrent use.
// Each operation retrieves a token with the operation context, sends one
// authenticated request to the configured GitHub endpoint, and decodes either
// its complete result or the GraphQL errors.
// GitHub responses containing both data and errors return only the errors;
// callers never observe a partial result.
type Gateway struct {
	// endpoint is the GraphQL endpoint derived from the configured API base URL.
	endpoint string

	// httpClient performs requests after the gateway has supplied authentication.
	httpClient *http.Client

	// tokens supplies credentials at the lifetime of each operation.
	tokens TokenSource
}

// NewGateway builds a client for a GitHub API base URL.
//
// apiURL is the REST API base URL reported by the forge configuration;
// NewGateway appends GitHub's GraphQL endpoint path.
// A nil HTTP client uses [http.DefaultClient].
// tokens is required and is consulted once for each operation.
func NewGateway(apiURL string, httpClient *http.Client, tokens TokenSource) (*Gateway, error) {
	endpoint, err := url.JoinPath(apiURL, "/graphql")
	if err != nil {
		return nil, fmt.Errorf("build GraphQL API URL: %w", err)
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if tokens == nil {
		return nil, errors.New("token source is required")
	}
	return &Gateway{
		endpoint:   endpoint,
		httpClient: httpClient,
		tokens:     tokens,
	}, nil
}

// graphQLRequestEnvelope is the JSON request shape accepted by GitHub's GraphQL API.
// Variables remains operation-specific so typed operations can own their wire
// fields without exposing generic GraphQL execution to callers.
type graphQLRequestEnvelope struct {
	Query     string `json:"query"`
	Variables any    `json:"variables"`
}

// graphQLResponseEnvelope separates the GraphQL operation result from protocol-level
// errors before operation-specific decoding.
// Keeping Data raw ensures errors take precedence over partial data.
type graphQLResponseEnvelope struct {
	Data   json.RawMessage `json:"data"`
	Errors graphQLError    `json:"errors"`
}

// execute performs the shared GraphQL request lifecycle for typed operations.
//
// query contains a GraphQL query or mutation. Query and variables must
// describe the operation's stable wire request, and
// result must be a pointer suitable for decoding the operation's data shape.
// execute retrieves credentials for every call so dynamic token sources retain
// their request lifetime.
// It owns response-body closure and converts transport, HTTP, protocol, and
// data-shape failures into errors with stage-specific context.
// A non-empty GraphQL error list always wins over Data to preserve the client's
// all-or-error result contract.
func (c *Gateway) execute(ctx context.Context, query string, variables, result any) error {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(graphQLRequestEnvelope{
		Query:     query,
		Variables: variables,
	}); err != nil {
		return fmt.Errorf("encode GraphQL request: %w", err)
	}

	token, err := c.tokens.Token(ctx)
	if err != nil {
		return fmt.Errorf("get GitHub token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, &body)
	if err != nil {
		return fmt.Errorf("build GraphQL request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send GraphQL request: %w", err)
	}
	// GraphQL envelopes are defined only for successful HTTP responses.
	// Bound diagnostics from other responses before closing the body so an
	// intermediary cannot make error reporting consume unbounded memory.
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		diagnostic, err := io.ReadAll(io.LimitReader(res.Body, maxErrorBody))
		err = errors.Join(err, res.Body.Close())
		if err != nil {
			return fmt.Errorf(
				"GitHub GraphQL HTTP status %s: read response: %w",
				res.Status,
				err,
			)
		}
		return fmt.Errorf(
			"GitHub GraphQL HTTP status %s: %s",
			res.Status,
			strings.TrimSpace(string(diagnostic)),
		)
	}

	responseBody, err := io.ReadAll(res.Body)
	err = errors.Join(err, res.Body.Close())
	if err != nil {
		return fmt.Errorf("read GraphQL response: %w", err)
	}

	var envelope graphQLResponseEnvelope
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return fmt.Errorf("decode GraphQL response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		return envelope.Errors
	}
	if err := json.Unmarshal(envelope.Data, result); err != nil {
		return fmt.Errorf("decode GraphQL data: %w", err)
	}
	return nil
}

func (c *Gateway) mutate(ctx context.Context, mutation string, input, result any) error {
	return c.execute(ctx, mutation, struct {
		Input any `json:"input"`
	}{input}, result)
}
