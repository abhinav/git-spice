// Package cloud provides a narrow Bitbucket REST client
// for the endpoints git-spice uses.
package cloud

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

const (
	_userAgent     = "git-spice"
	_defaultAPIURL = "https://api.bitbucket.org/2.0"
)

// ErrNotFound reports a Bitbucket 404 response.
var ErrNotFound = errors.New("404 Not Found")

// ErrDestinationBranchNotFound reports a pull request creation failure
// caused by a missing destination branch.
var ErrDestinationBranchNotFound = errors.New("destination branch not found")

// Client is a Bitbucket REST client specialized to the endpoints
// that git-spice uses.
type Client struct {
	httpClient *http.Client
	baseURL    string
	baseOrigin string
	basePath   string
	authHeader authHeaderFunc
}

// ClientOptions configures a Bitbucket REST client.
type ClientOptions struct {
	// BaseURL is the Bitbucket API base URL.
	//
	// If empty, the client uses Bitbucket Cloud at
	// `https://api.bitbucket.org/2.0`.
	BaseURL string

	// HTTPClient is the HTTP client used to send requests.
	//
	// If nil, the client uses [http.DefaultClient].
	HTTPClient *http.Client
}

// Token describes a Bitbucket credential.
type Token struct {
	AccessToken string
}

// TokenSource provides tokens for Bitbucket API requests.
type TokenSource interface {
	Token(context.Context) (Token, error)
}

// StaticTokenSource returns the same token on every request.
type StaticTokenSource Token

// Token implements [TokenSource].
func (s StaticTokenSource) Token(context.Context) (Token, error) {
	return Token(s), nil
}

// Response wraps the raw HTTP response and pagination state.
type Response struct {
	Header     http.Header
	StatusCode int
	NextURL    string
}

// NewClient builds a Bitbucket REST client.
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

	baseURL, baseOrigin, basePath, err := normalizeBaseURL(opts.BaseURL)
	if err != nil {
		return nil, err
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
		baseOrigin: baseOrigin,
		basePath:   basePath,
		authHeader: buildAuthHeader(tokenSource),
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

func normalizeBaseURL(baseURL string) (normalized, origin, path string, err error) {
	if baseURL == "" {
		baseURL = _defaultAPIURL
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		return "", "", "", fmt.Errorf("parse base URL: %w", err)
	}

	if u.Scheme == "" || u.Host == "" {
		return "", "", "", fmt.Errorf("invalid base URL %q", baseURL)
	}

	u.RawQuery = ""
	u.Fragment = ""
	u.Path = strings.TrimSuffix(u.Path, "/")

	originURL := *u
	originURL.Path = ""
	originURL.RawPath = ""

	return u.String(), originURL.String(), u.EscapedPath(), nil
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
// It owns the response body lifecycle and copies Bitbucket Cloud pagination
// state from decoded list responses into the returned Response.
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
	if next, ok := dst.(nextURLCarrier); ok {
		resp.NextURL = next.nextURL()
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

type nextURLCarrier interface {
	nextURL() string
}

func newResponse(resp *http.Response) *Response {
	return &Response{
		Header:     resp.Header.Clone(),
		StatusCode: resp.StatusCode,
	}
}

func (c *Client) resolveRequestURL(resourcePath string) (string, error) {
	u, err := url.Parse(resourcePath)
	if err != nil {
		return "", fmt.Errorf("parse request URL %q: %w", resourcePath, err)
	}

	if u.IsAbs() {
		return u.String(), nil
	}

	switch {
	case strings.HasPrefix(resourcePath, c.basePath+"/"):
		return c.baseOrigin + resourcePath, nil
	case strings.HasPrefix(resourcePath, "/"):
		return c.baseURL + resourcePath, nil
	default:
		return c.baseURL + "/" + strings.TrimLeft(resourcePath, "/"), nil
	}
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

type apiError struct {
	StatusCode int
	Method     string
	URL        string
	Body       []byte
}

func (e *apiError) Error() string {
	if len(bytes.TrimSpace(e.Body)) == 0 {
		return fmt.Sprintf("%s %s: %d", e.Method, e.URL, e.StatusCode)
	}
	return fmt.Sprintf(
		"%s %s: %d %s",
		e.Method,
		e.URL,
		e.StatusCode,
		strings.TrimSpace(string(e.Body)),
	)
}

// checkResponse converts unsuccessful HTTP responses into package errors.
//
// It reads the response body only when an error path needs response details.
func checkResponse(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusOK,
		http.StatusCreated,
		http.StatusAccepted,
		http.StatusNoContent:
		return nil
	case http.StatusNotFound:
		return ErrNotFound
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read error response: %w", err)
	}

	if isDestinationBranchNotFound(resp, body) {
		return ErrDestinationBranchNotFound
	}

	return &apiError{
		StatusCode: resp.StatusCode,
		Method:     resp.Request.Method,
		URL:        resp.Request.URL.String(),
		Body:       body,
	}
}

func isDestinationBranchNotFound(resp *http.Response, body []byte) bool {
	if resp.StatusCode != http.StatusBadRequest {
		return false
	}
	if resp.Request.Method != http.MethodPost {
		return false
	}
	if !strings.HasSuffix(resp.Request.URL.EscapedPath(), "/pullrequests") {
		return false
	}

	text := strings.ToLower(string(body))
	return strings.Contains(text, "destination") &&
		strings.Contains(text, "branch not found")
}
