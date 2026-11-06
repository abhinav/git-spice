package main

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/gitlab"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/giturl"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice"
)

func TestNewHTTPClient_gatewayHeaders(t *testing.T) {
	t.Setenv("GS_GATEWAY_TOKEN", "gateway-token")
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Cf-Access-Token") != "gateway-token" || r.Header.Get("X-Access-Client") != "git-spice" {
			http.Error(w, "gateway authentication required", http.StatusForbidden)
			return
		}
		if r.Header.Get("Private-Token") != "forge-token" {
			http.Error(w, "forge authentication required", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/user":
			_, _ = w.Write([]byte(`{"id":1,"username":"user"}`))
		case "/api/v4/projects/owner/repo":
			_, _ = w.Write([]byte(`{"id":123,"path_with_namespace":"owner/repo","default_branch":"main"}`))
		case "/api/v4/projects/123/merge_requests":
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Errorf("unexpected API request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// Configure the gateway headers through the same Git CLI used by users.
	configFile := filepath.Join(t.TempDir(), "gitconfig")
	entries := []git.ConfigEntry{
		{
			Key:   git.ConfigKey("spice.http." + server.URL + ".header"),
			Value: "X-Access-Client: git-spice",
		},
		{
			Key:   git.ConfigKey("spice.http." + server.URL + ".headerFromEnv"),
			Value: "cf-access-token: $GS_GATEWAY_TOKEN",
		},
	}
	for _, entry := range entries {
		cmd := exec.CommandContext(t.Context(), "git", "config", "--file", configFile,
			"--add", string(entry.Key), entry.Value)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
	}

	// Build the application's shared client and use it to open the forge.
	log := silogtest.New(t)
	gitConfig := git.NewConfig(git.ConfigOptions{
		Dir: filepath.Dir(configFile),
		Env: []string{
			"GIT_CONFIG_GLOBAL=" + configFile,
			"GIT_CONFIG_NOSYSTEM=1",
		},
		Log: log,
	})
	config, err := spice.LoadConfig(t.Context(), gitConfig, spice.ConfigOptions{Log: log})
	require.NoError(t, err)
	client, err := newHTTPClient(config, server.Client().Transport)
	require.NoError(t, err)

	remoteURL, err := giturl.Parse(server.URL + "/owner/repo")
	require.NoError(t, err)
	var registry forge.Registry
	registry.Register(&gitlab.Definition{
		Options: gitlab.Options{
			URL:    server.URL,
			APIURL: server.URL,
		},
		Log: log,
	})
	f, rid, ok := forge.InferFromRemoteURL(&registry, remoteURL, &forge.NewOptions{HTTPClient: client})
	require.True(t, ok)
	repo, err := f.OpenRepository(t.Context(), &gitlab.AuthenticationToken{
		AuthType:    gitlab.AuthTypePAT,
		AccessToken: "forge-token",
	}, rid)
	require.NoError(t, err)

	// Complete a repository API operation with both authentication layers.
	changes, err := repo.FindChangesByBranch(t.Context(), "feature", forge.FindChangesOptions{})
	require.NoError(t, err)
	assert.Empty(t, changes)
	assert.GreaterOrEqual(t, requests.Load(), int32(2))
}
