package bitbucket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"go.abhg.dev/gs/internal/silog"
)

// client is an HTTP client for the Bitbucket API.
type client struct {
	baseURL string
	token   *AuthenticationToken
	http    *http.Client
	log     *silog.Logger
}

func newClient(baseURL string, token *AuthenticationToken, log *silog.Logger) *client {
	return &client{
		baseURL: baseURL,
		token:   token,
		http:    http.DefaultClient,
		log:     log,
	}
}

func (c *client) url(path string) string {
	return c.baseURL + path
}

func (c *client) do(
	ctx context.Context,
	method, path string,
	body, result any,
) error {
	req, err := c.buildRequest(ctx, method, path, body)
	if err != nil {
		return err
	}

	respBody, err := c.executeRequest(req)
	if err != nil {
		return err
	}

	return decodeResponse(respBody, result)
}

func (c *client) buildRequest(
	ctx context.Context,
	method, path string,
	body any,
) (*http.Request, error) {
	reqBody, err := encodeBody(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, c.url(path), reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	c.setAuth(req)
	return req, nil
}

func encodeBody(body any) (io.Reader, error) {
	if body == nil {
		return nil, nil
	}
	bs, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request body: %w", err)
	}
	return bytes.NewReader(bs), nil
}

func (c *client) executeRequest(req *http.Request) ([]byte, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &apiError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}
	return respBody, nil
}

func decodeResponse(respBody []byte, result any) error {
	if result == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *client) setAuth(req *http.Request) {
	if c.token == nil || c.token.AccessToken == "" {
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.token.AccessToken)
}

func (c *client) get(ctx context.Context, path string, result any) error {
	return c.do(ctx, http.MethodGet, path, nil, result)
}

func (c *client) post(ctx context.Context, path string, body, result any) error {
	return c.do(ctx, http.MethodPost, path, body, result)
}

func (c *client) put(ctx context.Context, path string, body, result any) error {
	return c.do(ctx, http.MethodPut, path, body, result)
}

// apiError is an error returned by the Bitbucket API.
type apiError struct {
	StatusCode int
	Body       string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("bitbucket API error (status %d): %s", e.StatusCode, e.Body)
}
