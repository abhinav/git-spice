package forgejo_test

import (
	"cmp"
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgejo"
	"go.abhg.dev/gs/internal/forge/forgetest"
	gateway "go.abhg.dev/gs/internal/gateway/forgejo"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

// This file tests end-to-end interactions with the Forgejo API.
//
// In replay mode (default), tests run against committed VCR fixtures.
//
// To record fixtures against a live Docker Forgejo instance:
//
//	bash tools/record-forgejo-fixtures.sh
//
// To record fixtures against an existing Forgejo instance,
// configure the `forgejo` section in
// internal/forge/forgetest/testconfig.yaml,
// set FORGEJO_TOKEN and FORGEJO_FORK_TOKEN,
// and run:
//
//	FORGEJO_RECORD_MODE=existing bash tools/record-forgejo-fixtures.sh
func TestIntegration(t *testing.T) {
	requireFixtures(t)

	cfg, sanitizers := testConfig(t)
	remoteURL := cfg.URL + "/" + cfg.Owner + "/" + cfg.Repo + ".git"
	pushRemoteURL := cfg.URL + "/" + cfg.ForkOwner + "/" + cfg.ForkRepo + ".git"
	if forgetest.Update() {
		token := forgetest.Token(t, cfg.URL, "FORGEJO_TOKEN")
		remoteURL = gitAuthURL(cfg.URL, cfg.Owner, cfg.Repo, token)
		forkToken := forgetest.Token(t, cfg.URL, "FORGEJO_FORK_TOKEN")
		pushRemoteURL = gitAuthURL(
			cfg.URL,
			cfg.ForkOwner,
			cfg.ForkRepo,
			forkToken,
		)
	}

	forgejoForge := forgejo.Forge{
		Options: forgejo.Options{URL: cfg.URL},
		Log:     silogtest.New(t),
	}

	forgetest.RunIntegration(t, forgetest.IntegrationConfig{
		RemoteURL:     remoteURL,
		PushRemoteURL: pushRemoteURL,
		Forge:         &forgejoForge,
		Sanitizers:    sanitizers,
		OpenRepository: func(
			t *testing.T,
			httpClient *http.Client,
		) forge.Repository {
			token := forgetest.Token(t, cfg.URL, "FORGEJO_TOKEN")
			client, err := gateway.NewClient(
				gateway.StaticTokenSource(gateway.Token{
					Type:  gateway.TokenTypeAPIToken,
					Value: token,
				}),
				&gateway.ClientOptions{
					BaseURL:    cfg.URL,
					HTTPClient: httpClient,
				},
			)
			require.NoError(t, err)

			repo, err := forgejo.NewRepository(
				t.Context(),
				&forgejoForge,
				cfg.Owner,
				cfg.Repo,
				silogtest.New(t),
				client,
			)
			require.NoError(t, err)
			return repo
		},
		MergeChange: func(
			t *testing.T,
			repo forge.Repository,
			change forge.ChangeID,
		) {
			require.NoError(t,
				forgejo.MergeChange(
					t.Context(),
					repo.(*forgejo.Repository),
					change.(*forgejo.PR),
				))
		},
		CloseChange: func(
			t *testing.T,
			repo forge.Repository,
			change forge.ChangeID,
		) {
			require.NoError(t,
				forgejo.CloseChange(
					context.WithoutCancel(t.Context()),
					repo.(*forgejo.Repository),
					change.(*forgejo.PR),
				))
		},
		SetChangeCheck: func(
			t *testing.T,
			httpClient *http.Client,
			_ forge.Repository,
			_ forge.ChangeID,
			headHash git.Hash,
			check forge.ChangeCheck,
		) {
			token := forgetest.Token(t, cfg.URL, "FORGEJO_TOKEN")
			client, err := gateway.NewClient(
				gateway.StaticTokenSource(gateway.Token{
					Type:  gateway.TokenTypeAPIToken,
					Value: token,
				}),
				&gateway.ClientOptions{
					BaseURL:    cfg.URL,
					HTTPClient: httpClient,
				},
			)
			require.NoError(t, err)

			_, _, err = client.CommitStatusCreate(
				t.Context(),
				cfg.Owner,
				cfg.Repo,
				headHash.String(),
				&gateway.CreateStatusOption{
					State:       forgejoStatusState(check.State),
					Context:     check.Name,
					Description: "Synthetic status for git-spice integration tests",
				},
			)
			require.NoError(t, err)
		},
		Reviewers:           []string{cfg.Reviewer},
		Assignees:           []string{cfg.Assignee},
		SetCommentsPageSize: forgejo.SetChangeCommentsPageSize,
	})
}

func testConfig(
	t *testing.T,
) (cfg forgetest.ForgejoConfig, sanitizers []forgetest.Sanitizer) {
	t.Helper()

	canonical := forgetest.CanonicalForgejoConfig()
	if !forgetest.Update() {
		return canonical, nil
	}

	config := forgetest.Config(t).Forgejo
	cfg = forgetest.ForgejoConfig{
		URL: cmp.Or(os.Getenv("FORGEJO_URL"), config.URL, canonical.URL),
		ForgeConfig: forgetest.ForgeConfig{
			Owner:     config.Owner,
			Repo:      config.Repo,
			ForkOwner: config.ForkOwner,
			ForkRepo:  config.ForkRepo,
			Reviewer:  config.Reviewer,
			Assignee:  config.Assignee,
		},
	}

	return cfg, forgetest.ForgejoConfigSanitizers(cfg, canonical)
}

func requireFixtures(t *testing.T) {
	t.Helper()
	if forgetest.Update() {
		return
	}

	fixturesDir := filepath.Join("testdata", "fixtures", "TestIntegration")
	require.DirExists(t, fixturesDir,
		"Forgejo fixtures must be committed; "+
			"run tools/record-forgejo-fixtures.sh to record")
}

func gitAuthURL(baseURL, owner, repo, token string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return baseURL + "/" + owner + "/" + repo + ".git"
	}
	u.User = url.UserPassword(token, "")
	u.Path = strings.TrimRight(u.Path, "/") + "/" + owner + "/" + repo + ".git"
	return u.String()
}

func forgejoStatusState(state forge.ChangeCheckState) gateway.CommitStatusState {
	switch state {
	case forge.ChangeCheckPassed:
		return gateway.CommitStatusSuccess
	case forge.ChangeCheckFailed:
		return gateway.CommitStatusFailure
	default:
		return gateway.CommitStatusPending
	}
}
