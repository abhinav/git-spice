package forgejo_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgejo"
	"go.abhg.dev/gs/internal/forge/forgetest"
	gateway "go.abhg.dev/gs/internal/gateway/forgejo"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/httptest"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestIntegration(t *testing.T) {
	if !forgetest.Update() {
		t.Skip("Forgejo fixtures have not been recorded yet")
	}

	cfg, sanitizers := testConfig(t)
	remoteURL := forgejo.DefaultURL + "/" + cfg.Owner + "/" + cfg.Repo + ".git"

	forgejoForge := forgejo.Forge{
		Log: silogtest.New(t),
	}

	forgetest.RunIntegration(t, forgetest.IntegrationConfig{
		RemoteURL:  remoteURL,
		Forge:      &forgejoForge,
		Sanitizers: sanitizers,
		OpenRepository: func(
			t *testing.T,
			httpClient *http.Client,
		) forge.Repository {
			token := forgetest.Token(t, forgejo.DefaultURL, "FORGEJO_TOKEN")
			client, err := gateway.NewClient(
				gateway.StaticTokenSource(gateway.Token{
					Type:  gateway.TokenTypeAPIToken,
					Value: token,
				}),
				&gateway.ClientOptions{
					BaseURL:    forgejo.DefaultURL,
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
					t.Context(),
					repo.(*forgejo.Repository),
					change.(*forgejo.PR),
				))
		},
		SetChangeChecksState: func(
			t *testing.T,
			httpClient *http.Client,
			_ forge.Repository,
			_ forge.ChangeID,
			headHash git.Hash,
			state forge.ChecksState,
		) {
			token := forgetest.Token(t, forgejo.DefaultURL, "FORGEJO_TOKEN")
			client, err := gateway.NewClient(
				gateway.StaticTokenSource(gateway.Token{
					Type:  gateway.TokenTypeAPIToken,
					Value: token,
				}),
				&gateway.ClientOptions{
					BaseURL:    forgejo.DefaultURL,
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
					State:       forgejoStatusState(state),
					Context:     "git-spice integration",
					Description: "Synthetic status for git-spice integration tests",
				},
			)
			require.NoError(t, err)
		},
		Reviewers:             []string{cfg.Reviewer},
		Assignees:             []string{cfg.Assignee},
		SetCommentsPageSize:   forgejo.SetChangeCommentsPageSize,
		SkipCommentCounts:     true,
		SkipCommentPagination: true,
	})
}

func testConfig(
	t *testing.T,
) (cfg forgetest.ForgeConfig, sanitizers []httptest.Sanitizer) {
	config := forgetest.Config(t)
	cfg = config.Forgejo
	canonical := forgetest.CanonicalForgejoConfig()
	sanitizers = forgetest.ConfigSanitizers(cfg, canonical)
	return cfg, sanitizers
}

func forgejoStatusState(state forge.ChecksState) gateway.CommitStatusState {
	switch state {
	case forge.ChecksPassed:
		return gateway.CommitStatusSuccess
	case forge.ChecksFailed:
		return gateway.CommitStatusFailure
	default:
		return gateway.CommitStatusPending
	}
}
