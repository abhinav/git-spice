package github_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/fixturetest"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/forge/github"
	githubgateway "go.abhg.dev/gs/internal/gateway/github"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/httptest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"golang.org/x/oauth2"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

// This file tests basic, end-to-end interactions with the GitHub API
// using recorded fixtures.

var _fixtures = fixturetest.Config{Update: forgetest.Update}

// testConfig returns the GitHub test configuration and sanitizers for VCR fixtures.
// In update mode, loads from testconfig.yaml.
// In replay mode, returns canonical placeholders.
func testConfig(t *testing.T) (cfg forgetest.ForgeConfig, sanitizers []httptest.Sanitizer) {
	config := forgetest.Config(t)
	cfg = config.GitHub
	canonical := forgetest.CanonicalGitHubConfig()
	sanitizers = forgetest.ConfigSanitizers(cfg, canonical)
	return cfg, sanitizers
}

// TODO: delete newRecorder when tests have been migrated to forgetest.
func newRecorder(
	t *testing.T,
	name string,
	sanitizers []httptest.Sanitizer,
) *recorder.Recorder {
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("To update the test fixtures, run:")
			t.Logf("    GITHUB_TEST_OWNER=$owner GITHUB_TEST_REPO=$repo GITHUB_TOKEN=$token go test -update -run '^%s$'", t.Name())
		}
	})

	return forgetest.NewHTTPRecorder(t, name, sanitizers)
}

func newGateway(t *testing.T, httpClient *http.Client) *githubgateway.Gateway {
	t.Helper()
	client, err := githubgateway.NewGateway(
		github.DefaultAPIURL,
		&http.Client{Transport: httpClient.Transport},
		testTokenSource("token"),
	)
	require.NoError(t, err)
	return client
}

func TestIntegration_Repository(t *testing.T) {
	cfg, sanitizers := testConfig(t)
	remoteURL := "https://github.com/" + cfg.Owner + "/" + cfg.Repo
	rec := newRecorder(t, t.Name(), sanitizers)

	httpClient := rec.GetDefaultClient()
	token := forgetest.Token(t, remoteURL, "GITHUB_TOKEN")
	httpClient.Transport = &oauth2.Transport{
		Base:   httpClient.Transport,
		Source: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}),
	}

	gatewayClient := newGateway(t, httpClient)
	_, err := github.NewRepository(t.Context(), new(github.Forge), cfg.Owner, cfg.Repo, silogtest.New(t), gatewayClient, "")
	require.NoError(t, err)
}

func TestIntegration(t *testing.T) {
	cfg, sanitizers := testConfig(t)
	remoteURL := "https://github.com/" + cfg.Owner + "/" + cfg.Repo
	pushRemoteURL := "https://github.com/" + cfg.ForkOwner + "/" + cfg.ForkRepo

	t.Cleanup(func() {
		if t.Failed() && !forgetest.Update() {
			t.Logf("To update the test fixtures, run:")
			t.Logf("    Configure testconfig.yaml and run: GITHUB_TOKEN=$token go test -update -run '^%s$'", t.Name())
		}
	})

	githubForge := github.Forge{
		Log: silogtest.New(t),
	}

	forgetest.RunIntegration(t, forgetest.IntegrationConfig{
		RemoteURL:     remoteURL,
		PushRemoteURL: pushRemoteURL,
		Forge:         &githubForge,
		Sanitizers:    sanitizers,
		OpenRepository: func(t *testing.T, httpClient *http.Client) forge.Repository {
			token := forgetest.Token(t, remoteURL, "GITHUB_TOKEN")
			httpClient.Transport = &oauth2.Transport{
				Base: httpClient.Transport,
				Source: oauth2.StaticTokenSource(&oauth2.Token{
					AccessToken: token,
				}),
			}

			gatewayClient := newGateway(t, httpClient)
			newRepo, err := github.NewRepository(
				t.Context(), &githubForge, cfg.Owner, cfg.Repo,
				silogtest.New(t), gatewayClient, "",
			)
			require.NoError(t, err)
			return newRepo
		},
		CloseChange: func(t *testing.T, repo forge.Repository, change forge.ChangeID) {
			require.NoError(t, github.CloseChange(t.Context(), repo.(*github.Repository), change.(*github.PR)))
		},
		SetChangeCheck: func(
			t *testing.T,
			httpClient *http.Client,
			_ forge.Repository,
			_ forge.ChangeID,
			headHash git.Hash,
			check forge.ChangeCheck,
		) {
			require.NoError(t, setGitHubChangeChecksState(
				t.Context(),
				httpClient,
				cfg.Owner,
				cfg.Repo,
				headHash,
				check,
			))
		},
		SetCommentsPageSize: github.SetListChangeCommentsPageSize,
		Reviewers:           []string{cfg.Reviewer},
		Assignees:           []string{cfg.Assignee},
	})
}

func TestIntegration_Repository_LabelCreateDelete(t *testing.T) {
	cfg, sanitizers := testConfig(t)
	remoteURL := "https://github.com/" + cfg.Owner + "/" + cfg.Repo
	label := fixturetest.New(_fixtures, "label1", func() string { return randomString(8) }).Get(t)

	rec := newRecorder(t, t.Name(), sanitizers)
	httpClient := rec.GetDefaultClient()
	token := forgetest.Token(t, remoteURL, "GITHUB_TOKEN")
	httpClient.Transport = &oauth2.Transport{
		Base:   httpClient.Transport,
		Source: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}),
	}

	gatewayClient := newGateway(t, httpClient)
	repo, err := github.NewRepository(
		t.Context(), new(github.Forge), cfg.Owner, cfg.Repo, silogtest.New(t), gatewayClient, "",
	)
	require.NoError(t, err)

	id, err := repo.CreateLabel(t.Context(), label)
	require.NoError(t, err, "could not create label")
	t.Cleanup(func() {
		t.Logf("Deleting label: %s", label)
		ctx := context.WithoutCancel(t.Context())
		assert.NoError(t,
			repo.DeleteLabel(ctx, label), "could not delete label")
	})

	t.Run("createIsIdempotent", func(t *testing.T) {
		newID, err := repo.CreateLabel(t.Context(), label)
		require.NoError(t, err, "could not create label again")

		assert.Equal(t, id, newID, "label ID should be the same on idempotent create")
	})
}

func TestIntegration_Repository_notFoundError(t *testing.T) {
	cfg, sanitizers := testConfig(t)
	remoteURL := "https://github.com/" + cfg.Owner + "/" + cfg.Repo
	ctx := t.Context()
	rec := newRecorder(t, t.Name(), sanitizers)
	client := rec.GetDefaultClient()
	token := forgetest.Token(t, remoteURL, "GITHUB_TOKEN")
	client.Transport = &oauth2.Transport{
		Base:   client.Transport,
		Source: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}),
	}
	gatewayClient := newGateway(t, client)
	_, err := github.NewRepository(ctx, new(github.Forge), cfg.Owner, "does-not-exist-repo", silogtest.New(t), gatewayClient, "")
	require.Error(t, err)
	assert.ErrorIs(t, err, githubgateway.ErrNotFound)

	var gqlError *githubgateway.Error
	if assert.ErrorAs(t, err, &gqlError) {
		assert.Equal(t, "NOT_FOUND", gqlError.Type)
		assert.Equal(t, []any{"repository"}, gqlError.Path)
		assert.Contains(t, gqlError.Message, cfg.Owner+"/does-not-exist-repo")
	}
}

type testTokenSource string

func (s testTokenSource) Token(context.Context) (string, error) { return string(s), nil }

const _alnum = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// randomString generates a random alphanumeric string of length n.
func randomString(n int) string {
	b := make([]byte, n)
	for i := range b {
		var buf [1]byte
		_, _ = rand.Read(buf[:])
		idx := int(buf[0]) % len(_alnum)
		b[i] = _alnum[idx]
	}
	return string(b)
}

func setGitHubChangeChecksState(
	ctx context.Context,
	httpClient *http.Client,
	owner string,
	repo string,
	headHash git.Hash,
	check forge.ChangeCheck,
) error {
	// GitHub's GraphQL schema exposes the status rollup we read,
	// but commit status creation remains a REST API operation.
	// Check runs are a separate GitHub App-authenticated mechanism,
	// so these tests create classic commit statuses instead.
	body, err := json.Marshal(gitHubStatusRequest{
		State:       gitHubStatusState(check.State),
		Context:     check.Name,
		Description: "Synthetic status for git-spice integration tests",
	})
	if err != nil {
		return fmt.Errorf("marshal status: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf(
			"https://api.github.com/repos/%s/%s/statuses/%s",
			owner, repo, headHash,
		),
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("post status: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("post status: %s: %s", resp.Status, body)
	}
	return nil
}

type gitHubStatusRequest struct {
	State       string `json:"state"`
	Context     string `json:"context"`
	Description string `json:"description"`
}

func gitHubStatusState(state forge.ChangeCheckState) string {
	switch state {
	case forge.ChangeCheckPending:
		return "pending"
	case forge.ChangeCheckPassed:
		return "success"
	case forge.ChangeCheckFailed:
		return "failure"
	default:
		return "error"
	}
}
