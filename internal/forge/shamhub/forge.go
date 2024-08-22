package shamhub

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/forge"
)

// Options defines CLI options for the ShamHub forge.
type Options struct {
	// URL is the base URL for Git repositories
	// hosted on the ShamHub server.
	// URLs under this must implement the Git HTTP protocol.
	URL string `name:"shamhub-url" hidden:"" env:"SHAMHUB_URL" help:"Base URL for ShamHub requests"`

	// APIURL is the base URL for the ShamHub API.
	APIURL string `name:"shamhub-api-url" hidden:"" env:"SHAMHUB_API_URL" help:"Base URL for ShamHub API requests"`
}

// Forge provides an implementation of [forge.Forge] backed by a ShamHub
// server.
type Forge struct {
	Options

	// Log is the logger to use for logging.
	Log *log.Logger
}

var _ forge.Forge = (*Forge)(nil)

func (f *Forge) jsonHTTPClient() *jsonHTTPClient {
	return &jsonHTTPClient{
		log:    f.Log,
		client: http.DefaultClient,
	}
}

// ID reports a unique identifier for this forge.
func (*Forge) ID() string { return "shamhub" }

// CLIPlugin registers additional CLI flags for the ShamHub forge.
func (f *Forge) CLIPlugin() any { return &f.Options }

// MatchURL reports whether the given URL is a ShamHub URL.
func (f *Forge) MatchURL(remoteURL string) bool {
	if f.URL == "" {
		// ShamHub is not initialized.
		return false
	}

	_, ok := strings.CutPrefix(remoteURL, f.URL)
	return ok
}

type jsonHTTPClient struct {
	log     *log.Logger
	headers map[string]string
	client  interface {
		Do(*http.Request) (*http.Response, error)
	}
}

func (c *jsonHTTPClient) Get(ctx context.Context, url string, res any) error {
	return c.do(ctx, http.MethodGet, url, nil, res)
}

func (c *jsonHTTPClient) Post(ctx context.Context, url string, req, res any) error {
	return c.do(ctx, http.MethodPost, url, req, res)
}

func (c *jsonHTTPClient) Patch(ctx context.Context, url string, req, res any) error {
	return c.do(ctx, http.MethodPatch, url, req, res)
}

func (c *jsonHTTPClient) do(ctx context.Context, method, url string, req, res any) error {
	var reqBody io.Reader
	if req != nil {
		bs, err := json.Marshal(req)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(bs)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return fmt.Errorf("create HTTP request: %w", err)
	}
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}

	httpResp, err := c.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send HTTP request: %w", err)
	}
	defer func() {
		_ = httpResp.Body.Close()
	}()

	resBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d\nbody: %s", httpResp.StatusCode, resBody)
	}

	dec := json.NewDecoder(bytes.NewReader(resBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(res); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}
