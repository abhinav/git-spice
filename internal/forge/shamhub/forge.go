package shamhub

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/log"
	"go.abhg.dev/gs/internal/must"
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

// ParseRemoteURL parses the given remote URL and returns a [RepositoryID]
// for the repository if it matches the ShamHub URL.
func (f *Forge) ParseRemoteURL(remoteURL string) (forge.RepositoryID, error) {
	if f.URL == "" {
		return nil, fmt.Errorf("%w: ShamHub is not initialized", forge.ErrUnsupportedURL)
	}

	owner, repo, err := extractRepoInfo(f.URL, remoteURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", forge.ErrUnsupportedURL, err)
	}

	return &RepositoryID{
		url:   f.URL,
		owner: owner,
		repo:  repo,
	}, nil
}

// OpenRepository opens the repository that this repository ID points to.
func (f *Forge) OpenRepository(_ context.Context, token forge.AuthenticationToken, id forge.RepositoryID) (forge.Repository, error) {
	must.NotBeBlankf(f.URL, "URL is required")
	must.NotBeBlankf(f.APIURL, "API URL is required")

	rid := id.(*RepositoryID)
	tok := token.(*AuthenticationToken).tok
	client := f.jsonHTTPClient()
	client.headers = map[string]string{
		"Authentication-Token": tok,
	}

	apiURL, err := url.Parse(f.APIURL)
	if err != nil {
		return nil, fmt.Errorf("parse API URL: %w", err)
	}

	return &forgeRepository{
		forge:  f,
		owner:  rid.owner,
		repo:   rid.repo,
		apiURL: apiURL,
		log:    f.Log,
		client: client,
	}, nil
}

// RepositoryID is a unique identifier for a ShamHub repository.
type RepositoryID struct {
	url   string // required
	owner string // required
	repo  string // required
}

var _ forge.RepositoryID = (*RepositoryID)(nil)

// String returns a human-readable name for the repository ID.
func (rid *RepositoryID) String() string {
	return fmt.Sprintf("%s/%s", rid.owner, rid.repo)
}

// ChangeURL returns the URL at which the given change can be viewed.
func (rid *RepositoryID) ChangeURL(id forge.ChangeID) string {
	cr := id.(ChangeID)

	return fmt.Sprintf("%s/%s/%s/changes/%v", rid.url, rid.owner, rid.repo, int(cr))
}

func extractRepoInfo(forgeURL, remoteURL string) (owner, repo string, err error) {
	tail, ok := strings.CutPrefix(remoteURL, forgeURL)
	if !ok {
		return "", "", fmt.Errorf("unrecognized host: %v", remoteURL)
	}

	tail = strings.TrimSuffix(strings.TrimPrefix(tail, "/"), ".git")
	owner, repo, ok = strings.Cut(tail, "/")
	if !ok {
		return "", "", fmt.Errorf("no '/' found in %q", tail)
	}

	return owner, repo, nil
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
