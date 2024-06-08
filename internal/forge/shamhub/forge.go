package shamhub

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
)

// Forge provides an implementation of [forge.Forge] backed by a ShamHub
// server.
type Forge struct {
	// BaseURL is the base URL for Git repositories
	// hosted on the ShamHub server.
	// URLs under this must implement the Git HTTP protocol.
	URL string

	// APIURL is the base URL for the ShamHub API.
	APIURL string

	// Log is the logger to use for logging.
	Log *log.Logger
}

var _ forge.Forge = (*Forge)(nil)

// ID reports a unique identifier for this forge.
func (*Forge) ID() string { return "shamhub" }

// OpenURL opens a repository hosted on the forge with the given remote URL.
func (f *Forge) OpenURL(ctx context.Context, remoteURL string) (forge.Repository, error) {
	tail, ok := strings.CutPrefix(remoteURL, f.URL)
	if !ok {
		return nil, forge.ErrUnsupportedURL
	}

	tail = strings.TrimSuffix(strings.TrimPrefix(tail, "/"), ".git")
	owner, repo, ok := strings.Cut(tail, "/")
	if !ok {
		return nil, fmt.Errorf("%w: no '/' found in %q", forge.ErrUnsupportedURL, tail)
	}

	apiURL, err := url.Parse(f.APIURL)
	if err != nil {
		return nil, fmt.Errorf("parse API URL: %w", err)
	}

	return &forgeRepository{
		forge:  f,
		owner:  owner,
		repo:   repo,
		apiURL: apiURL,
		log:    f.Log,
		client: &jsonHTTPClient{
			client: http.DefaultClient,
			log:    f.Log,
		},
	}, nil
}

// forgeRepository is a repository hosted on a ShamHub server.
// It implements [forge.Repository].
type forgeRepository struct {
	forge  *Forge
	owner  string
	repo   string
	apiURL *url.URL
	log    *log.Logger
	client *jsonHTTPClient
}

var _ forge.Repository = (*forgeRepository)(nil)

func (f *forgeRepository) Forge() forge.Forge { return f.forge }

func (f *forgeRepository) SubmitChange(ctx context.Context, r forge.SubmitChangeRequest) (forge.SubmitChangeResult, error) {
	req := submitChangeRequest{
		Subject: r.Subject,
		Base:    r.Base,
		Body:    r.Body,
		Head:    r.Head,
		Draft:   r.Draft,
	}

	u := f.apiURL.JoinPath(f.owner, f.repo, "changes")
	var res submitChangeResponse
	if err := f.client.Post(ctx, u.String(), req, &res); err != nil {
		return forge.SubmitChangeResult{}, fmt.Errorf("submit change: %w", err)
	}

	return forge.SubmitChangeResult{
		ID:  forge.ChangeID(res.Number),
		URL: res.URL,
	}, nil
}

func (f *forgeRepository) FindChangesByBranch(ctx context.Context, branch string, opts forge.FindChangesOptions) ([]*forge.FindChangeItem, error) {
	if opts.Limit == 0 {
		opts.Limit = 10
	}

	u := f.apiURL.JoinPath(f.owner, f.repo, "changes", "by-branch", branch)
	q := u.Query()
	q.Set("limit", strconv.Itoa(opts.Limit))
	if opts.State == 0 {
		q.Set("state", "all")
	} else {
		q.Set("state", opts.State.String())
	}
	u.RawQuery = q.Encode()

	var res []*Change
	if err := f.client.Get(ctx, u.String(), &res); err != nil {
		return nil, fmt.Errorf("find changes by branch: %w", err)
	}

	changes := make([]*forge.FindChangeItem, len(res))
	for i, c := range res {
		var state forge.ChangeState
		switch c.State {
		case "open":
			state = forge.ChangeOpen
		case "closed":
			if c.Merged {
				state = forge.ChangeMerged
			} else {
				state = forge.ChangeClosed
			}
		}

		changes[i] = &forge.FindChangeItem{
			ID:       forge.ChangeID(c.Number),
			URL:      c.URL,
			State:    state,
			Subject:  c.Subject,
			HeadHash: git.Hash(c.Head.Hash),
			BaseName: c.Base.Name,
			Draft:    c.Draft,
		}
	}
	return changes, nil
}

func (f *forgeRepository) FindChangeByID(ctx context.Context, id forge.ChangeID) (*forge.FindChangeItem, error) {
	u := f.apiURL.JoinPath(f.owner, f.repo, "change", strconv.Itoa(int(id)))
	var res Change
	if err := f.client.Get(ctx, u.String(), &res); err != nil {
		return nil, fmt.Errorf("find change by ID: %w", err)
	}

	return &forge.FindChangeItem{
		ID:       forge.ChangeID(res.Number),
		URL:      res.URL,
		Subject:  res.Subject,
		HeadHash: git.Hash(res.Head.Hash),
		BaseName: res.Base.Name,
		Draft:    res.Draft,
	}, nil
}

func (f *forgeRepository) EditChange(ctx context.Context, id forge.ChangeID, opts forge.EditChangeOptions) error {
	var req editChangeRequest
	if opts.Base != "" {
		req.Base = &opts.Base
	}
	if opts.Draft != nil {
		req.Draft = opts.Draft
	}

	u := f.apiURL.JoinPath(f.owner, f.repo, "change", strconv.Itoa(int(id)))
	var res editChangeResponse
	if err := f.client.Patch(ctx, u.String(), req, &res); err != nil {
		return fmt.Errorf("edit change: %w", err)
	}

	return nil
}

func (f *forgeRepository) ChangeIsMerged(ctx context.Context, id forge.ChangeID) (bool, error) {
	u := f.apiURL.JoinPath(f.owner, f.repo, "change", strconv.Itoa(int(id)), "merged")
	var res isMergedResponse
	if err := f.client.Get(ctx, u.String(), &res); err != nil {
		return false, fmt.Errorf("is merged: %w", err)
	}
	return res.Merged, nil
}

var _changeTemplatePaths = []string{
	".shamhub/CHANGE_TEMPLATE.md",
	"CHANGE_TEMPLATE.md",
}

// ChangeTemplatePaths reports the case-insensitive paths at which
// it's possible to define change templates in the repository.
func (f *Forge) ChangeTemplatePaths() []string {
	return slices.Clone(_changeTemplatePaths)
}

func (f *forgeRepository) ListChangeTemplates(ctx context.Context) ([]*forge.ChangeTemplate, error) {
	u := f.apiURL.JoinPath(f.owner, f.repo, "change-template")
	var res changeTemplateResponse
	if err := f.client.Get(ctx, u.String(), &res); err != nil {
		return nil, fmt.Errorf("lookup change body template: %w", err)
	}

	out := make([]*forge.ChangeTemplate, len(res))
	for i, t := range res {
		out[i] = &forge.ChangeTemplate{
			Filename: t.Filename,
			Body:     t.Body,
		}
	}

	return out, nil
}

type jsonHTTPClient struct {
	log    *log.Logger
	client interface {
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
