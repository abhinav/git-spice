// Package gitea provides a narrow Gitea REST client
// for the endpoints git-spice uses.
package gitea

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	_giteaUserAgent = "git-spice"
	_apiVersionPath = "api/v1/"
)

// ErrMergeNotReady reports a Gitea 405 response on a merge endpoint.
//
// Gitea returns 405 "Please try again later" when mergeability of a
// pull request has not yet been computed.
// Callers should retry the merge after a short delay.
var ErrMergeNotReady = errors.New("405 Method Not Allowed")

// ErrNotFound reports a Gitea 404 response.
//
// Callers use this sentinel
// to translate Gitea's "not found" response
// into forge-level not-found handling
// without inspecting raw HTTP state.
var ErrNotFound = errors.New("404 Not Found")

// Client is a Gitea REST client
// specialized to the endpoints
// that git-spice actually uses.
//
// It is intentionally not a general-purpose Gitea client.
// The package only needs a narrow slice of the API surface,
// so the client stays purpose-built
// around those endpoints.
type Client struct {
	httpClient *http.Client

	// baseURL is the normalized API root, always ending in `/api/v1/`.
	baseURL string

	// authHeader supplies the per-request authentication header.
	authHeader authHeaderFunc
}

// ClientOptions configures a Gitea REST client.
type ClientOptions struct {
	// BaseURL is the Gitea host URL or API root URL.
	//
	// The value may be either a host URL such as
	// `https://gitea.example.com`
	// or an explicit API URL such as
	// `https://gitea.example.com/api/v1`.
	// It is normalized to an API root ending in `/api/v1/`.
	BaseURL string

	// HTTPClient is the HTTP client used to send requests.
	//
	// If nil, the client uses [http.DefaultClient].
	HTTPClient *http.Client
}

// TokenType identifies how a token should be attached to a Gitea request.
type TokenType int

const (
	// TokenTypeToken sends the token
	// in Gitea's `Authorization: token` header.
	TokenTypeToken TokenType = iota

	// TokenTypeBearer sends the token
	// as an `Authorization: Bearer` header.
	TokenTypeBearer
)

// Token describes a Gitea credential.
type Token struct {
	Type  TokenType
	Value string
}

// TokenSource provides tokens for Gitea API requests.
type TokenSource interface {
	Token(context.Context) (Token, error)
}

// StaticTokenSource returns the same token on every request.
type StaticTokenSource Token

// Token implements [TokenSource].
func (s StaticTokenSource) Token(context.Context) (Token, error) {
	return Token(s), nil
}

// NewClient builds a Gitea REST client.
func NewClient(
	tokenSource TokenSource,
	opts *ClientOptions,
) (*Client, error) {
	if tokenSource == nil {
		return nil, errors.New("nil token source")
	}

	opts = cmp.Or(opts, &ClientOptions{})

	if opts.BaseURL == "" {
		return nil, errors.New("gitea base URL is required")
	}

	authHeader, err := buildAuthHeader(tokenSource)
	if err != nil {
		return nil, err
	}

	normalizedBaseURL, err := normalizeBaseURL(opts.BaseURL)
	if err != nil {
		return nil, err
	}

	httpClient := cmp.Or(opts.HTTPClient, http.DefaultClient)

	return &Client{
		httpClient: httpClient,
		baseURL:    normalizedBaseURL,
		authHeader: authHeader,
	}, nil
}

// authHeaderFunc returns the authentication header for a request.
type authHeaderFunc func(context.Context) (http.Header, error)

// buildAuthHeader selects the Gitea authentication scheme
// that matches the configured token source.
func buildAuthHeader(tokenSource TokenSource) (authHeaderFunc, error) {
	return func(ctx context.Context) (http.Header, error) {
		token, err := tokenSource.Token(ctx)
		if err != nil {
			return nil, err
		}

		header := make(http.Header, 1)
		switch token.Type {
		case TokenTypeToken:
			header.Set("Authorization", "token "+token.Value)
		case TokenTypeBearer:
			header.Set("Authorization", "Bearer "+token.Value)
		default:
			return nil, fmt.Errorf(
				"no source for authentication type: %v",
				token.Type,
			)
		}
		return header, nil
	}, nil
}

// normalizeBaseURL converts user configuration
// into a canonical Gitea API root.
//
// Inputs may be either a Gitea host URL
// like `https://gitea.example.com`
// or an explicit API URL
// like `https://gitea.example.com/api/v1`.
// The returned URL always ends with `/api/v1/`.
func normalizeBaseURL(baseURL string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse base URL: %w", err)
	}

	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid base URL %q", baseURL)
	}

	path := strings.TrimPrefix(u.Path, "/")
	switch {
	case path == "":
		u.Path = "/" + _apiVersionPath
	case strings.HasSuffix(path, _apiVersionPath):
		u.Path = "/" + path
	case strings.HasSuffix(path, strings.TrimSuffix(_apiVersionPath, "/")):
		u.Path = "/" + path + "/"
	default:
		u.Path = "/" + strings.TrimSuffix(path, "/") + "/" + _apiVersionPath
	}

	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// get sends a Gitea GET request.
func (c *Client) get(
	ctx context.Context,
	resourcePath string,
	query url.Values,
	dst any,
) (*Response, error) {
	return c.do(ctx, http.MethodGet, resourcePath, query, nil, dst)
}

// post sends a Gitea POST request.
func (c *Client) post(
	ctx context.Context,
	resourcePath string,
	query url.Values,
	body any,
	dst any,
) (*Response, error) {
	return c.do(ctx, http.MethodPost, resourcePath, query, body, dst)
}

// patch sends a Gitea PATCH request.
func (c *Client) patch(
	ctx context.Context,
	resourcePath string,
	query url.Values,
	body any,
	dst any,
) (*Response, error) {
	return c.do(ctx, http.MethodPatch, resourcePath, query, body, dst)
}

// delete sends a Gitea DELETE request.
func (c *Client) delete(
	ctx context.Context,
	resourcePath string,
	query url.Values,
) (*Response, error) {
	return c.do(ctx, http.MethodDelete, resourcePath, query, nil, nil)
}

// do sends a Gitea API request and decodes the response.
func (c *Client) do(
	ctx context.Context,
	method string,
	resourcePath string,
	query url.Values,
	body any,
	dst any,
) (*Response, error) {
	reqURL := c.baseURL + resourcePath
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
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
	req.Header.Set("User-Agent", _giteaUserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.authHeader != nil {
		header, err := c.authHeader(ctx)
		if err != nil {
			return nil, err
		}
		for key, values := range header {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
	}

	httpResp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, httpResp.Body)
		_ = httpResp.Body.Close()
	}()

	resp := newResponse(httpResp)
	if err := checkResponse(httpResp); err != nil {
		return resp, err
	}

	if dst == nil || httpResp.StatusCode == http.StatusNoContent {
		return resp, nil
	}

	if err := json.NewDecoder(httpResp.Body).Decode(dst); err != nil {
		return resp, fmt.Errorf("decode response: %w", err)
	}
	return resp, nil
}

// Response wraps the raw HTTP response
// and exposes Gitea's pagination headers in parsed form.
type Response struct {
	Header       http.Header
	StatusCode   int
	ItemsPerPage int
	CurrentPage  int
	NextPage     int
	PrevPage     int
	TotalPages   int
	TotalItems   int
}

func newResponse(resp *http.Response) *Response {
	response := &Response{
		Header:     resp.Header.Clone(),
		StatusCode: resp.StatusCode,
	}
	response.TotalItems = parseHeaderInt(resp.Header.Get("X-Total"))
	response.TotalPages = parseHeaderInt(resp.Header.Get("X-Total-Pages"))
	response.ItemsPerPage = parseHeaderInt(resp.Header.Get("X-Per-Page"))
	response.CurrentPage = parseHeaderInt(resp.Header.Get("X-Page"))
	response.NextPage = parseHeaderInt(resp.Header.Get("X-Next-Page"))
	response.PrevPage = parseHeaderInt(resp.Header.Get("X-Prev-Page"))
	return response
}

func parseHeaderInt(s string) int {
	if s == "" {
		return 0
	}
	v, _ := strconv.Atoi(s)
	return v
}

// APIError captures a non-success Gitea response.
type APIError struct {
	StatusCode int
	Message    string
	Body       []byte

	method string
	url    string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf(
			"%s %s: %d",
			e.method,
			e.url,
			e.StatusCode,
		)
	}
	return fmt.Sprintf(
		"%s %s: %d %s",
		e.method,
		e.url,
		e.StatusCode,
		e.Message,
	)
}

// checkResponse converts Gitea HTTP failures into package errors.
func checkResponse(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusOK,
		http.StatusCreated,
		http.StatusAccepted,
		http.StatusNoContent,
		http.StatusNotModified:
		return nil
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusMethodNotAllowed:
		return ErrMergeNotReady
	}

	path := resp.Request.URL.RawPath
	if path == "" {
		path = resp.Request.URL.EscapedPath()
	}

	errResp := &APIError{
		StatusCode: resp.StatusCode,
		method:     resp.Request.Method,
		url: fmt.Sprintf(
			"%s://%s%s",
			resp.Request.URL.Scheme,
			resp.Request.URL.Host,
			path,
		),
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return errResp
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return errResp
	}

	errResp.Body = body

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		errResp.Message = string(body)
		return errResp
	}

	// Gitea error body: {"message": "...", "url": "..."}
	if msg, ok := raw["message"].(string); ok {
		errResp.Message = msg
	}
	return errResp
}
