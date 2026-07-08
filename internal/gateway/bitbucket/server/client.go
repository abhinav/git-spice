// Package server provides a narrow Bitbucket Data Center/Server
// REST 1.0 client for the endpoints git-spice uses.
//
// Data Center serves several API roots (pull requests, build status,
// default reviewers) under one origin; the client routes calls to each.
package server

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
	"sync"
)

const _userAgent = "git-spice"

// Client is a Bitbucket Data Center/Server REST 1.0 client
// specialized to the endpoints that git-spice uses.
type Client struct {
	httpClient           *http.Client
	baseURL              string
	buildBase            string
	defaultReviewersBase string
	authHeader           authHeaderFunc

	// appProps memoizes the server descriptor; see [Client.ApplicationProperties].
	appPropsMu sync.Mutex
	appProps   *ApplicationProperties
}

// ClientOptions configures a Bitbucket Data Center/Server REST client.
type ClientOptions struct {
	// BaseURL is the Bitbucket Data Center REST API base URL.
	//
	// This is required; there is no default host.
	BaseURL string

	// HTTPClient is the HTTP client used to send requests.
	//
	// If nil, the client uses [http.DefaultClient].
	HTTPClient *http.Client
}

// Token describes a Bitbucket Data Center credential.
type Token struct {
	// AccessToken is the HTTP access token (PAT) sent as a Bearer token.
	AccessToken string
}

// TokenSource provides tokens for Bitbucket Data Center API requests.
type TokenSource interface {
	Token(context.Context) (Token, error)
}

// StaticTokenSource returns the same token on every request.
type StaticTokenSource Token

// Token implements [TokenSource].
func (s StaticTokenSource) Token(context.Context) (Token, error) {
	return Token(s), nil
}

// Response wraps the parts of an HTTP response the client exposes.
type Response struct {
	StatusCode int

	// AUserName is the X-AUSERNAME header: the authenticated username,
	// which Bitbucket Data Center sets on successful requests.
	AUserName string
}

// NewClient builds a Bitbucket Data Center/Server REST client.
func NewClient(
	tokenSource TokenSource,
	opts *ClientOptions,
) (*Client, error) {
	if tokenSource == nil {
		return nil, errors.New("nil token source")
	}

	if opts == nil {
		opts = &ClientOptions{}
	}

	baseURL, err := normalizeBaseURL(opts.BaseURL)
	if err != nil {
		return nil, err
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &Client{
		httpClient:           httpClient,
		baseURL:              baseURL,
		buildBase:            swapAPIRoot(baseURL, "/rest/build-status/1.0"),
		defaultReviewersBase: swapAPIRoot(baseURL, "/rest/default-reviewers/1.0"),
		authHeader:           buildAuthHeader(tokenSource),
	}, nil
}

type authHeaderFunc func(context.Context) (http.Header, error)

func buildAuthHeader(tokenSource TokenSource) authHeaderFunc {
	return func(ctx context.Context) (http.Header, error) {
		token, err := tokenSource.Token(ctx)
		if err != nil {
			return nil, err
		}

		header := make(http.Header, 1)
		if token.AccessToken != "" {
			header.Set("Authorization", "Bearer "+token.AccessToken)
		}
		return header, nil
	}
}

func normalizeBaseURL(baseURL string) (string, error) {
	if baseURL == "" {
		return "", errors.New("base URL is required")
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse base URL: %w", err)
	}

	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid base URL %q", baseURL)
	}

	u.RawQuery = ""
	u.Fragment = ""
	u.Path = strings.TrimSuffix(u.Path, "/")

	return u.String(), nil
}

// swapAPIRoot derives a sibling REST root from the API base URL by swapping
// its "/rest/api/1.0" suffix for root (e.g. "/rest/build-status/1.0").
// A base URL without the standard suffix gets root appended instead.
func swapAPIRoot(baseURL, root string) string {
	if trimmed, ok := strings.CutSuffix(baseURL, "/rest/api/1.0"); ok {
		return trimmed + root
	}
	return baseURL + root
}

func (c *Client) get(
	ctx context.Context,
	resourcePath string,
	query url.Values,
	dst any,
) (*Response, error) {
	return c.doJSON(ctx, http.MethodGet, resourcePath, query, nil, dst)
}

func (c *Client) post(
	ctx context.Context,
	resourcePath string,
	query url.Values,
	body any,
	dst any,
) (*Response, error) {
	return c.doJSON(ctx, http.MethodPost, resourcePath, query, body, dst)
}

func (c *Client) put(
	ctx context.Context,
	resourcePath string,
	query url.Values,
	body any,
	dst any,
) (*Response, error) {
	return c.doJSON(ctx, http.MethodPut, resourcePath, query, body, dst)
}

func (c *Client) delete(
	ctx context.Context,
	resourcePath string,
	query url.Values,
) (*Response, error) {
	return c.doJSON(ctx, http.MethodDelete, resourcePath, query, nil, nil)
}

// doJSON sends a JSON API request and decodes a successful JSON response.
//
// It owns the response body lifecycle for JSON endpoints.
func (c *Client) doJSON(
	ctx context.Context,
	method string,
	resourcePath string,
	query url.Values,
	body any,
	dst any,
) (*Response, error) {
	reqURL, err := c.resolveRequestURL(resourcePath)
	if err != nil {
		return nil, err
	}

	if len(query) > 0 {
		reqURL, err = mergeQuery(reqURL, query)
		if err != nil {
			return nil, err
		}
	}

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	httpResp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer drainAndClose(httpResp.Body)

	resp := newResponse(httpResp)
	if err := checkResponse(httpResp); err != nil {
		return resp, err
	}

	if dst == nil || httpResp.StatusCode == http.StatusNoContent {
		return resp, nil
	}

	if err := json.NewDecoder(httpResp.Body).Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return resp, nil
		}
		return resp, fmt.Errorf("decode response: %w", err)
	}
	return resp, nil
}

// getRaw sends a GET request for a resource that is not JSON-encoded
// and returns the raw response body.
func (c *Client) getRaw(
	ctx context.Context,
	resourcePath string,
) ([]byte, *Response, error) {
	reqURL, err := c.resolveRequestURL(resourcePath)
	if err != nil {
		return nil, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build request: %w", err)
	}

	httpResp, err := c.do(req)
	if err != nil {
		return nil, nil, err
	}
	defer drainAndClose(httpResp.Body)

	resp := newResponse(httpResp)
	if err := checkResponse(httpResp); err != nil {
		return nil, resp, err
	}

	bodyBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, resp, fmt.Errorf("read response: %w", err)
	}
	return bodyBytes, resp, nil
}

// do applies client-wide headers and sends req.
//
// The caller owns the returned response body.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", _userAgent)

	header, err := c.authHeader(req.Context())
	if err != nil {
		return nil, err
	}
	for key, values := range header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	httpResp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	return httpResp, nil
}

func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}

func newResponse(resp *http.Response) *Response {
	return &Response{
		StatusCode: resp.StatusCode,
		AUserName:  resp.Header.Get("X-AUSERNAME"),
	}
}

func (c *Client) resolveRequestURL(resourcePath string) (string, error) {
	u, err := url.Parse(resourcePath)
	if err != nil {
		return "", fmt.Errorf("parse request URL %q: %w", resourcePath, err)
	}

	// Absolute URLs target a non-default API root,
	// e.g. build-status or default-reviewers.
	if u.IsAbs() {
		return u.String(), nil
	}

	if strings.HasPrefix(resourcePath, "/") {
		return c.baseURL + resourcePath, nil
	}
	return c.baseURL + "/" + resourcePath, nil
}

func mergeQuery(reqURL string, query url.Values) (string, error) {
	u, err := url.Parse(reqURL)
	if err != nil {
		return "", fmt.Errorf("parse request URL %q: %w", reqURL, err)
	}

	values := u.Query()
	for key, items := range query {
		for _, item := range items {
			values.Add(key, item)
		}
	}
	u.RawQuery = values.Encode()
	return u.String(), nil
}
