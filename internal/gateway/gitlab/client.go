// Package gitlab provides a narrow GitLab REST client
// for the endpoints git-spice uses.
package gitlab

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
	_gitLabUserAgent = "git-spice"
	_apiVersionPath  = "api/v4/"
	_defaultURL      = "https://gitlab.com"
)

// ErrNotFound reports a GitLab 404 response.
//
// Callers use this sentinel
// to translate GitLab's "not found" response
// into forge-level not-found handling
// without inspecting raw HTTP state.
//
// GitLab REST troubleshooting:
// https://docs.gitlab.com/api/rest/troubleshooting/
var ErrNotFound = errors.New("404 Not Found")

// Client is a GitLab REST client
// specialized to the endpoints
// that git-spice actually uses.
//
// It is intentionally not a general-purpose GitLab client.
// The package only needs a narrow slice of the API surface,
// so the client stays purpose-built
// around those endpoints.
type Client struct {
	httpClient *http.Client

	// baseURL is the normalized API root, always ending in `/api/v4/`.
	baseURL string

	// authHeader supplies the per-request authentication header.
	authHeader authHeaderFunc
}

// ClientOptions configures a GitLab REST client.
type ClientOptions struct {
	// BaseURL is the GitLab host URL or API root URL.
	//
	// If empty, the client uses GitLab SaaS at `https://gitlab.com`.
	// The value may be either a host URL such as
	// `https://gitlab.example.com`
	// or an explicit API URL such as
	// `https://gitlab.example.com/api/v4`.
	// It is normalized to an API root ending in `/api/v4/`.
	BaseURL string

	// HTTPClient is the HTTP client used to send requests.
	//
	// If nil, the client uses [http.DefaultClient].
	HTTPClient *http.Client
}

// TokenType identifies how a token should be attached to a GitLab request.
type TokenType int

const (
	// TokenTypePrivateToken sends the token
	// in GitLab's `Private-Token` header.
	TokenTypePrivateToken TokenType = iota

	// TokenTypeBearer sends the token
	// as an `Authorization: Bearer` header.
	TokenTypeBearer
)

// Token describes a GitLab credential.
type Token struct {
	Type  TokenType
	Value string
}

// TokenSource provides tokens for GitLab API requests.
type TokenSource interface {
	Token(context.Context) (Token, error)
}

// StaticTokenSource returns the same token on every request.
type StaticTokenSource Token

// Token implements [TokenSource].
func (s StaticTokenSource) Token(context.Context) (Token, error) {
	return Token(s), nil
}

// NewClient builds a GitLab REST client.
func NewClient(
	tokenSource TokenSource,
	opts *ClientOptions,
) (*Client, error) {
	if tokenSource == nil {
		return nil, errors.New("nil token source")
	}

	opts = cmp.Or(opts, &ClientOptions{})

	authHeader, err := buildAuthHeader(tokenSource)
	if err != nil {
		return nil, err
	}

	normalizedBaseURL, err := normalizeBaseURL(opts.BaseURL)
	if err != nil {
		return nil, err
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &Client{
		httpClient: httpClient,
		baseURL:    normalizedBaseURL,
		authHeader: authHeader,
	}, nil
}

// authHeaderFunc returns the authentication header for a request.
//
// GitLab supports different authentication schemes
// depending on how the token was obtained.
// Resolving that once up front
// keeps request execution simple
// and lets `Client`
// treat authentication as a single injected capability.
//
// GitLab REST authentication:
// https://docs.gitlab.com/api/rest/authentication/
type authHeaderFunc func(context.Context) (http.Header, error)

// buildAuthHeader selects the GitLab authentication scheme
// that matches the configured token source.
//
// The returned closure hides the differences
// between GitLab personal access tokens,
// OAuth bearer tokens,
// and tokens sourced from the GitLab CLI,
// so the request helpers only need to ask
// for "the auth header" at send time.
//
// GitLab REST authentication:
// https://docs.gitlab.com/api/rest/authentication/
func buildAuthHeader(
	tokenSource TokenSource,
) (authHeaderFunc, error) {
	return func(ctx context.Context) (http.Header, error) {
		token, err := tokenSource.Token(ctx)
		if err != nil {
			return nil, err
		}

		header := make(http.Header, 1)
		switch token.Type {
		case TokenTypePrivateToken:
			header.Set("Private-Token", token.Value)
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
// into a canonical GitLab API root.
//
// Inputs may be either a GitLab host URL
// like `https://gitlab.example.com`
// or an explicit API URL
// like `https://gitlab.example.com/api/v4`.
// The returned URL is always stripped of query and fragment components
// and always ends with `/api/v4/`,
// which lets request helpers append endpoint paths directly
// without needing to reason about slashes
// or whether the caller supplied the host URL
// or the API URL.
//
// This normalization also preserves GitLab Enterprise compatibility
// because self-managed instances use the same API path shape
// under a different host.
func normalizeBaseURL(baseURL string) (string, error) {
	if baseURL == "" {
		baseURL = _defaultURL
	}

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

// get sends a GitLab GET request.
//
// `resourcePath` is the endpoint path relative to the normalized API root,
// for example `projects/42/merge_requests`.
// `query` contains already-encoded query parameters for the request.
// `dst` is the response value to JSON-decode into.
func (c *Client) get(
	ctx context.Context,
	resourcePath string,
	query url.Values,
	dst any,
) (*Response, error) {
	return c.do(ctx, http.MethodGet, resourcePath, query, nil, dst)
}

// post sends a GitLab POST request.
//
// `resourcePath` is the endpoint path relative to the normalized API root.
// `query` contains already-encoded query parameters for the request.
// `body` is the request payload, which will be JSON-encoded.
// `dst` is the response value to JSON-decode into.
func (c *Client) post(
	ctx context.Context,
	resourcePath string,
	query url.Values,
	body any,
	dst any,
) (*Response, error) {
	return c.do(ctx, http.MethodPost, resourcePath, query, body, dst)
}

// put sends a GitLab PUT request.
//
// `resourcePath` is the endpoint path relative to the normalized API root.
// `query` contains already-encoded query parameters for the request.
// `body` is the request payload, which will be JSON-encoded.
// `dst` is the response value to JSON-decode into.
func (c *Client) put(
	ctx context.Context,
	resourcePath string,
	query url.Values,
	body any,
	dst any,
) (*Response, error) {
	return c.do(ctx, http.MethodPut, resourcePath, query, body, dst)
}

// delete sends a GitLab DELETE request.
//
// `resourcePath` is the endpoint path relative to the normalized API root.
// `query` contains already-encoded query parameters for the request.
func (c *Client) delete(
	ctx context.Context,
	resourcePath string,
	query url.Values,
) (*Response, error) {
	return c.do(ctx, http.MethodDelete, resourcePath, query, nil, nil)
}

// do sends a GitLab API request and decodes the response.
//
// `method` is the HTTP verb to use.
// `resourcePath` is the endpoint path relative to the normalized API root.
// `query` contains already-encoded query parameters for the request.
// `body` is the request payload, if any, which will be JSON-encoded.
// `dst` is the response value to decode into. If `dst` is nil, the response
// body is ignored after error handling.
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
	req.Header.Set("User-Agent", _gitLabUserAgent)
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
// and exposes GitLab's offset-pagination headers in parsed form.
//
// These fields correspond to the `X-Total`,
// `X-Total-Pages`,
// `X-Per-Page`,
// `X-Page`,
// `X-Next-Page`,
// and `X-Prev-Page` headers
// that GitLab uses for many REST list endpoints.
// The forge layer only needs simple page-following,
// so the client parses those headers once
// rather than repeating string conversion in callers.
//
// GitLab pagination:
// https://docs.gitlab.com/api/rest/#pagination
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

// APIError captures a non-success GitLab response.
//
// `Body` preserves the raw response for debugging.
// `Response` preserves the status code and request metadata.
// `Message` stores the best-effort flattened error text
// extracted from GitLab's JSON error payload.
//
// GitLab REST troubleshooting:
// https://docs.gitlab.com/api/rest/troubleshooting/
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

// checkResponse converts GitLab HTTP failures into package errors.
//
// `404 Not Found` is returned
// as the sentinel `errGitLabNotFound`
// so callers can treat missing resources specially.
// Other failures are converted into `gitlabAPIError`,
// including a parsed best-effort message
// when GitLab returns a structured JSON error body.
//
// GitLab REST troubleshooting:
// https://docs.gitlab.com/api/rest/troubleshooting/
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

	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		errResp.Message = fmt.Sprintf(
			"failed to parse unknown error format: %s",
			body,
		)
		return errResp
	}

	errResp.Message = parseError(raw)
	return errResp
}

// parseError flattens the range of JSON error payloads
// GitLab returns
// into a single readable message.
//
// GitLab error bodies are not uniform.
// Some endpoints return a top-level `"message"` string,
// some return `"message"` objects keyed by field name,
// and some nest arrays and objects several levels deep.
// Flattening those shapes in one place
// keeps `checkResponse` simple
// and produces stable, readable error text
// for callers and tests.
//
// GitLab REST troubleshooting:
// https://docs.gitlab.com/api/rest/troubleshooting/
func parseError(v any) string {
	switch v := v.(type) {
	case map[string]any:
		if msg, ok := v["message"]; ok {
			return parseError(msg)
		}

		var parts []string
		for key, value := range v {
			message := parseError(value)
			if message == "" {
				continue
			}
			parts = append(parts, key+": "+message)
		}
		return strings.Join(parts, ", ")

	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if msg := parseError(item); msg != "" {
				parts = append(parts, msg)
			}
		}
		return strings.Join(parts, ", ")

	case string:
		return v

	default:
		return fmt.Sprint(v)
	}
}
