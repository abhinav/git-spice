package bitbucket_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/bitbucket"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/httptest"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

// This file tests basic, end-to-end interactions
// with the Bitbucket API using recorded fixtures.

// testConfig returns the Bitbucket test configuration
// and sanitizers for VCR fixtures.
// In update mode, loads from testconfig.yaml.
// In replay mode, returns canonical placeholders.
func testConfig(
	t *testing.T,
) (cfg forgetest.ForgeConfig, sanitizers []httptest.Sanitizer) {
	config := forgetest.Config(t)
	cfg = config.Bitbucket
	canonical := forgetest.CanonicalBitbucketConfig()
	sanitizers = forgetest.ConfigSanitizers(cfg, canonical)
	return cfg, sanitizers
}

func TestIntegration(t *testing.T) {
	cfg, sanitizers := testConfig(t)
	remoteURL := bitbucket.DefaultURL +
		"/" + cfg.Owner + "/" + cfg.Repo + ".git"
	t.Cleanup(func() {
		if t.Failed() && !forgetest.Update() {
			t.Logf("To update the test fixtures, run:")
			t.Logf("    Configure testconfig.yaml and run: "+
				"BITBUCKET_TOKEN=<your-token> "+
				"go test -update -run '^%s$'", t.Name())
		}
	})

	bitbucketForge := bitbucket.Forge{
		Log: silogtest.New(t),
	}

	forgetest.RunIntegration(t, forgetest.IntegrationConfig{
		RemoteURL: remoteURL,
		// TODO:
		// Uncomment this to record BitBucket fixtures.
		// Unfortunately, I don't have a BitBucket account,
		// and have been unable to set one up successfully.
		// PushRemoteURL: bitbucket.DefaultURL + "/" + cfg.ForkOwner + "/" + cfg.ForkRepo + ".git",
		Forge:      &bitbucketForge,
		Sanitizers: sanitizers,
		OpenRepository: func(
			t *testing.T, httpClient *http.Client,
		) forge.Repository {
			_, token, source := forgetest.Credential(
				t, remoteURL,
				"BITBUCKET_EMAIL", "BITBUCKET_TOKEN",
			)
			authType := bitbucket.AuthTypeAPIToken
			if source == forgetest.CredentialSourceGCM {
				authType = bitbucket.AuthTypeGCM
			}
			return bitbucket.NewRepositoryForTest(
				&bitbucketForge,
				bitbucket.DefaultURL,
				cfg.Owner, cfg.Repo,
				silogtest.New(t),
				httpClient,
				&bitbucket.AuthenticationToken{
					AuthType:    authType,
					AccessToken: token,
				},
			)
		},
		MergeChange: func(
			t *testing.T,
			repo forge.Repository,
			change forge.ChangeID,
		) {
			require.NoError(t,
				bitbucket.MergeChange(
					t.Context(),
					repo.(*bitbucket.Repository),
					change.(*bitbucket.PR),
				))
		},
		CloseChange: func(
			t *testing.T,
			repo forge.Repository,
			change forge.ChangeID,
		) {
			require.NoError(t,
				bitbucket.CloseChange(
					t.Context(),
					repo.(*bitbucket.Repository),
					change.(*bitbucket.PR),
				))
		},
		SetCommentsPageSize: bitbucket.SetListChangeCommentsPageSize,
		Reviewers:           []string{cfg.Reviewer},
		Assignees:           []string{},
		// Bitbucket limitations:
		SkipLabels:            true, // no PR labels
		SkipAssignees:         true, // no PR assignees
		SkipTemplates:         true, // limited template support
		ShortHeadHash:         true, // API returns 12-char hashes
		SkipMerge:             true, // requires branch permissions
		SkipCommentPagination: true, // 403 with small pages
	})
}
