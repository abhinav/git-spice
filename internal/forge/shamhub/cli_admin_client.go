package shamhub

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

// shamhubCLIAdminClient is the CLI's REST transport for ShamHub admin routes.
type shamhubCLIAdminClient struct {
	apiURL string
	gitURL string
	token  string
	client *http.Client
}

func newShamHubCLIAdminClient(
	getenv func(string) string,
) (*shamhubCLIAdminClient, error) {
	apiURL := getenv("SHAMHUB_API_URL")
	if apiURL == "" {
		return nil, errors.New("SHAMHUB_API_URL is required")
	}
	if _, err := url.Parse(apiURL); err != nil {
		return nil, fmt.Errorf("parse SHAMHUB_API_URL: %w", err)
	}

	gitURL := getenv("SHAMHUB_URL")
	if gitURL == "" {
		return nil, errors.New("SHAMHUB_URL is required")
	}
	if _, err := url.Parse(gitURL); err != nil {
		return nil, fmt.Errorf("parse SHAMHUB_URL: %w", err)
	}

	token := getenv("SHAMHUB_ADMIN_TOKEN")
	if token == "" {
		return nil, errors.New("SHAMHUB_ADMIN_TOKEN is required")
	}

	return &shamhubCLIAdminClient{
		apiURL: strings.TrimRight(apiURL, "/"),
		gitURL: strings.TrimRight(gitURL, "/"),
		token:  token,
		client: http.DefaultClient,
	}, nil
}

// Get sends an authenticated GET request to a ShamHub admin endpoint.
func (c *shamhubCLIAdminClient) Get(
	ctx context.Context,
	path string,
	res any,
) error {
	return c.do(ctx, http.MethodGet, path, nil, res)
}

// Post sends an authenticated POST request to a ShamHub admin endpoint.
func (c *shamhubCLIAdminClient) Post(
	ctx context.Context,
	path string,
	req any,
	res any,
) error {
	return c.do(ctx, http.MethodPost, path, req, res)
}

// Patch sends an authenticated PATCH request to a ShamHub admin endpoint.
func (c *shamhubCLIAdminClient) Patch(
	ctx context.Context,
	path string,
	req any,
	res any,
) error {
	return c.do(ctx, http.MethodPatch, path, req, res)
}

// Delete sends an authenticated DELETE request to a ShamHub admin endpoint.
func (c *shamhubCLIAdminClient) Delete(
	ctx context.Context,
	path string,
	res any,
) error {
	return c.do(ctx, http.MethodDelete, path, nil, res)
}

func (c *shamhubCLIAdminClient) do(
	ctx context.Context,
	method string,
	path string,
	req any,
	res any,
) error {
	var body io.Reader
	if req != nil {
		bs, err := json.Marshal(req)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		body = bytes.NewReader(bs)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx,
		method,
		c.apiURL+path,
		body,
	)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("ShamHub-Admin-Token", c.token)

	httpResp, err := c.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer func() {
		_ = httpResp.Body.Close()
	}()

	resBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d: %s", httpResp.StatusCode, resBody)
	}
	if res == nil {
		return nil
	}
	if err := json.Unmarshal(resBody, res); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
