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
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/secret"
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

// AuthenticationToken defines the token returned by the ShamHub forge.
type AuthenticationToken struct {
	forge.AuthenticationToken

	tok string
}

// AuthenticationFlow initiates the authentication flow for the ShamHub forge.
// The flow is optimized for ease of use from test scripts
// and is not representative of a real-world authentication flow.
//
// To authenticate, the user must set the SHAMHUB_USERNAME environment variable
// before attempting to authenticate.
// The flow will fail if these variables are not set.
// The flow will also fail if the user is already authenticated.
func (f *Forge) AuthenticationFlow(ctx context.Context) (forge.AuthenticationToken, error) {
	must.NotBeBlankf(f.APIURL, "API URL is required")

	username := os.Getenv("SHAMHUB_USERNAME")
	if username == "" {
		return nil, errors.New("SHAMHUB_USERNAME is required")
	}

	loginURL, err := url.JoinPath(f.APIURL, "/login")
	if err != nil {
		return nil, fmt.Errorf("parse API URL: %w", err)
	}

	req := loginRequest{
		Username: username,
	}
	var res loginResponse
	if err := f.jsonHTTPClient().Post(ctx, loginURL, req, &res); err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}

	return &AuthenticationToken{tok: res.Token}, nil
}

func (f *Forge) secretService() string {
	must.NotBeBlankf(f.URL, "URL is required")
	return "shamhub:" + f.URL
}

// SaveAuthenticationToken saves the given authentication token to the stash.
func (f *Forge) SaveAuthenticationToken(stash secret.Stash, t forge.AuthenticationToken) error {
	return stash.SaveSecret(f.secretService(), "token", t.(*AuthenticationToken).tok)
}

// LoadAuthenticationToken loads the authentication token from the stash.
func (f *Forge) LoadAuthenticationToken(stash secret.Stash) (forge.AuthenticationToken, error) {
	token, err := stash.LoadSecret(f.secretService(), "token")
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}
	return &AuthenticationToken{tok: token}, nil
}

// ClearAuthenticationToken removes the authentication token from the stash.
func (f *Forge) ClearAuthenticationToken(stash secret.Stash) error {
	return stash.DeleteSecret(f.secretService(), "token")
}

// ChangeMetadata records the metadata for a change on a ShamHub server.
type ChangeMetadata struct {
	Number int `json:"number"`
}

// ForgeID reports the forge ID that owns this metadata.
func (*ChangeMetadata) ForgeID() string {
	return "shamhub" // TODO: const
}

// ChangeID reports the change ID of the change.
func (m *ChangeMetadata) ChangeID() forge.ChangeID {
	return ChangeID(m.Number)
}

// NewChangeMetadata returns the metadata for a change on a ShamHub server.
func (f *forgeRepository) NewChangeMetadata(ctx context.Context, id forge.ChangeID) (forge.ChangeMetadata, error) {
	return &ChangeMetadata{Number: int(id.(ChangeID))}, nil
}

// MarshalChangeMetadata marshals the given change metadata to JSON.
func (f *Forge) MarshalChangeMetadata(md forge.ChangeMetadata) (json.RawMessage, error) {
	return json.Marshal(md)
}

// UnmarshalChangeMetadata unmarshals the given JSON data to change metadata.
func (f *Forge) UnmarshalChangeMetadata(data json.RawMessage) (forge.ChangeMetadata, error) {
	var md ChangeMetadata
	if err := json.Unmarshal(data, &md); err != nil {
		return nil, fmt.Errorf("unmarshal change metadata: %w", err)
	}
	return &md, nil
}

// ChangeID is a unique identifier for a change on a ShamHub server.
type ChangeID int

var _ forge.ChangeID = ChangeID(0)

func (id ChangeID) String() string { return fmt.Sprintf("#%d", id) }

// MatchURL reports whether the given URL is a ShamHub URL.
func (f *Forge) MatchURL(remoteURL string) bool {
	must.NotBeBlankf(f.URL, "URL is required")

	_, ok := strings.CutPrefix(remoteURL, f.URL)
	return ok
}

// OpenURL opens a repository hosted on the forge with the given remote URL.
func (f *Forge) OpenURL(ctx context.Context, token forge.AuthenticationToken, remoteURL string) (forge.Repository, error) {
	must.NotBeBlankf(f.URL, "URL is required")
	must.NotBeBlankf(f.APIURL, "API URL is required")

	tok := token.(*AuthenticationToken).tok
	client := f.jsonHTTPClient()
	client.headers = map[string]string{
		"Authentication-Token": tok,
	}

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
		client: client,
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
		ID:  ChangeID(res.Number),
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
			ID:       ChangeID(c.Number),
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

func (f *forgeRepository) FindChangeByID(ctx context.Context, fid forge.ChangeID) (*forge.FindChangeItem, error) {
	id := fid.(ChangeID)
	u := f.apiURL.JoinPath(f.owner, f.repo, "change", strconv.Itoa(int(id)))
	var res Change
	if err := f.client.Get(ctx, u.String(), &res); err != nil {
		return nil, fmt.Errorf("find change by ID: %w", err)
	}

	return &forge.FindChangeItem{
		ID:       ChangeID(res.Number),
		URL:      res.URL,
		Subject:  res.Subject,
		HeadHash: git.Hash(res.Head.Hash),
		BaseName: res.Base.Name,
		Draft:    res.Draft,
	}, nil
}

func (f *forgeRepository) EditChange(ctx context.Context, fid forge.ChangeID, opts forge.EditChangeOptions) error {
	var req editChangeRequest
	if opts.Base != "" {
		req.Base = &opts.Base
	}
	if opts.Draft != nil {
		req.Draft = opts.Draft
	}

	id := fid.(ChangeID)
	u := f.apiURL.JoinPath(f.owner, f.repo, "change", strconv.Itoa(int(id)))
	var res editChangeResponse
	if err := f.client.Patch(ctx, u.String(), req, &res); err != nil {
		return fmt.Errorf("edit change: %w", err)
	}

	return nil
}

func (f *forgeRepository) ChangeIsMerged(ctx context.Context, fid forge.ChangeID) (bool, error) {
	id := fid.(ChangeID)
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
