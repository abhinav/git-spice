// Package forgejo provides a narrow Forgejo REST client
// for the endpoints git-spice uses.
package forgejo

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
	_userAgent      = "git-spice"
	_apiVersionPath = "api/v1/"
	_defaultURL     = "https://codeberg.org"
)

// ErrNotFound reports a Forgejo 404 response.
//
// Callers use this sentinel to translate Forgejo's "not found" response
// into forge-level not-found handling without inspecting raw HTTP state.
//
// Forgejo API:
// https://forgejo.org/docs/v10.0/user/api-usage/
var ErrNotFound = errors.New("404 Not Found")

// Client is a Forgejo REST client specialized to the endpoints
// that git-spice uses.
//
// It is intentionally not a general-purpose Forgejo client.
// The package only models the API surface needed by the forge layer.
type Client struct {
	httpClient *http.Client

	// baseURL is the normalized API root, always ending in `/api/v1/`.
	baseURL string

	// authHeader supplies the per-request authentication header.
	authHeader authHeaderFunc
}

// ClientOptions configures a Forgejo REST client.
type ClientOptions struct {
	// BaseURL is the Forgejo host URL or API root URL.
	//
	// If empty, the client uses Codeberg at `https://codeberg.org`.
	// The value may be either a host URL such as
	// `https://forgejo.example.com`
	// or an explicit API URL such as
	// `https://forgejo.example.com/api/v1`.
	// It is normalized to an API root ending in `/api/v1/`.
	BaseURL string

	// HTTPClient is the HTTP client used to send requests.
	//
	// If nil, the client uses [http.DefaultClient].
	HTTPClient *http.Client
}

// TokenType identifies how a token should be attached to a Forgejo request.
type TokenType int

const (
	// TokenTypeAPIToken sends the token
	// as an `Authorization: token` header.
	TokenTypeAPIToken TokenType = iota

	// TokenTypeBearer sends the token
	// as an `Authorization: bearer` header.
	TokenTypeBearer
)

// Token describes a Forgejo credential.
type Token struct {
	// Type selects the Forgejo authentication scheme.
	Type TokenType

	// Value is the raw token value.
	Value string
}

// TokenSource provides tokens for Forgejo API requests.
type TokenSource interface {
	Token(context.Context) (Token, error)
}

// StaticTokenSource returns the same token on every request.
type StaticTokenSource Token

// Token implements [TokenSource].
func (s StaticTokenSource) Token(context.Context) (Token, error) {
	return Token(s), nil
}

// NewClient builds a Forgejo REST client.
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
		return nil, fmt.Errorf("build auth header: %w", err)
	}

	normalizedBaseURL, err := normalizeBaseURL(opts.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("normalize base URL: %w", err)
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
// Forgejo keeps API-token and OAuth2 bearer authentication
// as distinct wire formats.
// Resolving that distinction once up front keeps request execution simple.
//
// Forgejo API:
// https://forgejo.org/docs/v10.0/user/api-usage/#authentication
type authHeaderFunc func(context.Context) (http.Header, error)

func buildAuthHeader(tokenSource TokenSource) (authHeaderFunc, error) {
	return func(ctx context.Context) (http.Header, error) {
		token, err := tokenSource.Token(ctx)
		if err != nil {
			return nil, err
		}

		header := make(http.Header, 1)
		switch token.Type {
		case TokenTypeAPIToken:
			header.Set("Authorization", "token "+token.Value)
		case TokenTypeBearer:
			header.Set("Authorization", "bearer "+token.Value)
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
// into a canonical Forgejo API root.
//
// Inputs may be either a Forgejo host URL
// like `https://forgejo.example.com`
// or an explicit API URL
// like `https://forgejo.example.com/api/v1`.
// The returned URL is always stripped of query and fragment components
// and always ends with `/api/v1/`,
// which lets request helpers append endpoint paths directly.
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

func (c *Client) patch(
	ctx context.Context,
	resourcePath string,
	query url.Values,
	body any,
	dst any,
) (*Response, error) {
	return c.do(ctx, http.MethodPatch, resourcePath, query, body, dst)
}

func (c *Client) delete(
	ctx context.Context,
	resourcePath string,
	query url.Values,
) (*Response, error) {
	return c.do(ctx, http.MethodDelete, resourcePath, query, nil, nil)
}

func (c *Client) deleteWithBody(
	ctx context.Context,
	resourcePath string,
	query url.Values,
	body any,
) (*Response, error) {
	return c.do(ctx, http.MethodDelete, resourcePath, query, body, nil)
}

func (c *Client) do(
	ctx context.Context,
	method string,
	resourcePath string,
	query url.Values,
	body any,
	dst any,
) (*Response, error) {
	reqURL := c.baseURL + strings.TrimLeft(resourcePath, "/")
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
	req.Header.Set("User-Agent", _userAgent)
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
		return nil, fmt.Errorf("send request: %w", err)
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

// Response wraps the raw HTTP response and exposes Forgejo pagination state.
//
// Forgejo uses `Link` headers for pagination
// and `x-total-count` for total item counts.
// The forge layer only needs simple page-following,
// so the client parses those headers once.
//
// Forgejo API:
// https://forgejo.org/docs/v10.0/user/api-usage/#pagination
type Response struct {
	// Header is the response header set.
	Header http.Header

	// StatusCode is the HTTP status code.
	StatusCode int

	// NextPage is the next `page` value from a pagination link.
	NextPage int

	// TotalItems is the total item count reported by Forgejo.
	TotalItems int
}

func newResponse(resp *http.Response) *Response {
	return &Response{
		Header:     resp.Header.Clone(),
		StatusCode: resp.StatusCode,
		NextPage:   parseNextPage(resp.Header.Get("Link")),
		TotalItems: parseHeaderInt(resp.Header.Get("x-total-count")),
	}
}

func parseHeaderInt(s string) int {
	if s == "" {
		return 0
	}
	v, _ := strconv.Atoi(s)
	return v
}

func parseNextPage(linkHeader string) int {
	for link := range strings.SplitSeq(linkHeader, ",") {
		parts := strings.Split(link, ";")
		if len(parts) < 2 {
			continue
		}

		if strings.TrimSpace(parts[1]) != `rel="next"` {
			continue
		}

		rawURL := strings.Trim(strings.TrimSpace(parts[0]), "<>")
		u, err := url.Parse(rawURL)
		if err != nil {
			return 0
		}
		return parseHeaderInt(u.Query().Get("page"))
	}
	return 0
}

// APIError captures a non-success Forgejo response.
//
// Body preserves the raw response for debugging.
// Message stores the best-effort error text extracted from Forgejo's JSON
// error payload.
type APIError struct {
	// StatusCode is the HTTP status code from Forgejo.
	StatusCode int

	// Message is the best-effort parsed Forgejo error text.
	Message string

	// Body is the raw response body.
	Body []byte

	method string
	url    string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("%s %s: %d", e.method, e.url, e.StatusCode)
	}
	return fmt.Sprintf(
		"%s %s: %d %s",
		e.method,
		e.url,
		e.StatusCode,
		e.Message,
	)
}

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

	var raw struct {
		Message any `json:"message"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		errResp.Message = fmt.Sprintf(
			"failed to parse unknown error format: %s",
			body,
		)
		return errResp
	}

	errResp.Message = parseErrorMessage(raw.Message)
	return errResp
}

func parseErrorMessage(raw any) string {
	switch v := raw.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			parts = append(parts, parseErrorMessage(item))
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		var parts []string
		for key, value := range v {
			if msg := parseErrorMessage(value); msg != "" {
				parts = append(parts, key+": "+msg)
			}
		}
		return strings.Join(parts, ", ")
	default:
		return fmt.Sprint(v)
	}
}
