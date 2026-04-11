// Package bitbucket provides a narrow Bitbucket REST client
// for the endpoints git-spice uses.
package bitbucket

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
	return c.do(ctx, http.MethodGet, resourcePath, query, nil, dst)
}

func (c *Client) post(
	ctx context.Context,
	resourcePath string,
	query url.Values,
	body any,
	dst any,
) (*Response, error) {
	return c.do(ctx, http.MethodPost, resourcePath, query, body, dst)
}

func (c *Client) put(
	ctx context.Context,
	resourcePath string,
	query url.Values,
	body any,
	dst any,
) (*Response, error) {
	return c.do(ctx, http.MethodPut, resourcePath, query, body, dst)
}

func (c *Client) delete(
	ctx context.Context,
	resourcePath string,
	query url.Values,
) (*Response, error) {
	return c.do(ctx, http.MethodDelete, resourcePath, query, nil, nil)
}

func (c *Client) do(
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
	req.Header.Set("User-Agent", _userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	header, err := c.authHeader(ctx)
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
	defer func() {
		_, _ = io.Copy(io.Discard, httpResp.Body)
		_ = httpResp.Body.Close()
	}()

	bodyBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	resp := &Response{
		Header:     httpResp.Header.Clone(),
		StatusCode: httpResp.StatusCode,
		NextURL:    extractNextURL(bodyBytes),
	}
	if err := checkResponse(httpResp, bodyBytes); err != nil {
		return resp, err
	}

	if dst == nil || httpResp.StatusCode == http.StatusNoContent ||
		len(bytes.TrimSpace(bodyBytes)) == 0 {
		return resp, nil
	}

	if err := json.Unmarshal(bodyBytes, dst); err != nil {
		return resp, fmt.Errorf("decode response: %w", err)
	}
	return resp, nil
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

func extractNextURL(body []byte) string {
	var next struct {
		Next string `json:"next"`
	}
	if err := json.Unmarshal(body, &next); err != nil {
		return ""
	}
	return next.Next
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

func checkResponse(resp *http.Response, body []byte) error {
	switch resp.StatusCode {
	case http.StatusOK,
		http.StatusCreated,
		http.StatusAccepted,
		http.StatusNoContent:
		return nil
	case http.StatusNotFound:
		return ErrNotFound
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
