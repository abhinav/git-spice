package gitea_test

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	giteaforge "go.abhg.dev/gs/internal/forge/gitea"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/httptest"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

// This file tests end-to-end interactions with the Gitea API.
//
// In replay mode (default), tests run against committed VCR fixtures.
// If no fixtures exist, the tests are skipped.
//
// To record fixtures against a live Gitea instance (e.g., Docker):
//
//	GITEA_URL=http://localhost:3000 \
//	GITEA_TOKEN=<token> \
//	GITEA_TEST_OWNER=testadmin \
//	GITEA_TEST_REPO=test-repo \
//	GITEA_TEST_FORK_OWNER=test-reviewer \
//	GITEA_TEST_FORK_REPO=test-fork-repo \
//	GITEA_TEST_REVIEWER=test-reviewer \
//	GITEA_TEST_ASSIGNEE=test-reviewer \
//	go test -update -run TestIntegration ./internal/forge/gitea/...
//
// The CI test-gitea-live job in .github/workflows/ci.yml runs this
// in update mode against a Docker Gitea container on every PR.

// testConfig returns the Gitea test configuration and sanitizers for VCR fixtures.
func testConfig(t *testing.T) (cfg forgetest.GiteaConfig, sanitizers []httptest.Sanitizer) {
	t.Helper()
	if !forgetest.Update() {
		return forgetest.CanonicalGiteaConfig(), nil
	}

	canonical := forgetest.CanonicalGiteaConfig()

	cfg = forgetest.GiteaConfig{
		URL: cmp(os.Getenv("GITEA_URL"), canonical.URL),
		ForgeConfig: forgetest.ForgeConfig{
			Owner:     cmp(os.Getenv("GITEA_TEST_OWNER"), canonical.Owner),
			Repo:      cmp(os.Getenv("GITEA_TEST_REPO"), canonical.Repo),
			ForkOwner: cmp(os.Getenv("GITEA_TEST_FORK_OWNER"), canonical.ForkOwner),
			ForkRepo:  cmp(os.Getenv("GITEA_TEST_FORK_REPO"), canonical.ForkRepo),
			Reviewer:  cmp(os.Getenv("GITEA_TEST_REVIEWER"), canonical.Reviewer),
			Assignee:  cmp(os.Getenv("GITEA_TEST_ASSIGNEE"), canonical.Assignee),
		},
	}

	sanitizers = []httptest.Sanitizer{}
	for _, pair := range [][2]string{
		{cfg.URL, canonical.URL},
		{cfg.Owner, canonical.Owner},
		{cfg.Repo, canonical.Repo},
		{cfg.ForkOwner, canonical.ForkOwner},
		{cfg.ForkRepo, canonical.ForkRepo},
		{cfg.Reviewer, canonical.Reviewer},
		{cfg.Assignee, canonical.Assignee},
	} {
		actual, canon := pair[0], pair[1]
		if actual != "" && actual != canon {
			sanitizers = append(sanitizers, httptest.Sanitizer{
				Replace: actual,
				With:    canon,
			})
		}
	}

	return cfg, sanitizers
}

func cmp(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func newTestGiteaClient(t *testing.T, cfg forgetest.GiteaConfig, httpClient *http.Client) *giteagw.Client {
	t.Helper()
	token := forgetest.Token(t, cfg.URL, "GITEA_TOKEN")
	gc, err := giteaforge.NewGiteaClient(token, cfg.URL, httpClient)
	require.NoError(t, err)
	return gc
}

// skipIfNoFixtures skips the test when not in update mode and no VCR fixtures
// exist for this test. This prevents failures when running the test suite
// locally without a Gitea instance.
func skipIfNoFixtures(t *testing.T) {
	t.Helper()
	if forgetest.Update() {
		return
	}
	fixturesDir := filepath.Join("testdata", t.Name())
	if _, err := os.Stat(fixturesDir); os.IsNotExist(err) {
		t.Skipf("no VCR fixtures at %s -- run with -update against a Gitea instance to record", fixturesDir)
	}
}

func TestIntegration_Repository(t *testing.T) {
	skipIfNoFixtures(t)
	cfg, sanitizers := testConfig(t)
	rec := forgetest.NewHTTPRecorder(t, t.Name(), sanitizers)
	gc := newTestGiteaClient(t, cfg, rec.GetDefaultClient())

	f := &giteaforge.Forge{
		Options: giteaforge.Options{URL: cfg.URL},
		Log:     silogtest.New(t),
	}

	_, err := giteaforge.NewRepository(
		t.Context(), f, cfg.Owner, cfg.Repo, silogtest.New(t), gc,
	)
	require.NoError(t, err)
}

func TestIntegration_Repository_notFoundError(t *testing.T) {
	skipIfNoFixtures(t)
	cfg, sanitizers := testConfig(t)
	rec := forgetest.NewHTTPRecorder(t, t.Name(), sanitizers)
	gc := newTestGiteaClient(t, cfg, rec.GetDefaultClient())

	f := &giteaforge.Forge{
		Options: giteaforge.Options{URL: cfg.URL},
		Log:     silogtest.New(t),
	}

	_, err := giteaforge.NewRepository(
		t.Context(), f, cfg.Owner, "does-not-exist-repo-xyz", silogtest.New(t), gc,
	)
	require.Error(t, err)
}

func TestIntegration(t *testing.T) {
	skipIfNoFixtures(t)
	cfg, sanitizers := testConfig(t)

	t.Cleanup(func() {
		if t.Failed() && !forgetest.Update() {
			t.Logf("To update the test fixtures, run:")
			t.Logf("  GITEA_URL=http://localhost:3000 GITEA_TOKEN=<token> ... go test -update -run '^%s$'", t.Name())
		}
	})

	remoteURL := cfg.URL + "/" + cfg.Owner + "/" + cfg.Repo

	// Fork PR tests require:
	// - A real fork of the base repo owned by ForkOwner
	// - Push credentials for the fork (GITEA_FORK_TOKEN)
	// Set GITEA_FORK_TOKEN to enable fork PR recording.
	// Without it, fork tests are skipped in both update and replay modes.
	forkToken := os.Getenv("GITEA_FORK_TOKEN")
	var pushRemoteURL string

	if forgetest.Update() {
		token := forgetest.Token(t, cfg.URL, "GITEA_TOKEN")
		remoteURL = gitAuthURL(cfg.URL, cfg.Owner, cfg.Repo, token)
		if forkToken != "" {
			pushRemoteURL = gitAuthURL(cfg.URL, cfg.ForkOwner, cfg.ForkRepo, forkToken)
		}
	} else if forkToken != "" {
		// Only run fork tests in replay mode if we also have the token
		// (i.e., fixtures were previously recorded with it).
		pushRemoteURL = cfg.URL + "/" + cfg.ForkOwner + "/" + cfg.ForkRepo
	}

	giteaForge := &giteaforge.Forge{
		Options: giteaforge.Options{URL: cfg.URL},
		Log:     silogtest.New(t),
	}

	forgetest.RunIntegration(t, forgetest.IntegrationConfig{
		RemoteURL:     remoteURL,
		PushRemoteURL: pushRemoteURL,
		Forge:         giteaForge,
		Sanitizers:    sanitizers,

		OpenRepository: func(t *testing.T, httpClient *http.Client) forge.Repository {
			gc := newTestGiteaClient(t, cfg, httpClient)
			repo, err := giteaforge.NewRepository(
				t.Context(), giteaForge, cfg.Owner, cfg.Repo,
				silogtest.New(t), gc,
			)
			require.NoError(t, err)
			return repo
		},

		MergeChange: func(t *testing.T, repo forge.Repository, changeID forge.ChangeID) {
			require.NoError(t,
				repo.MergeChange(t.Context(), changeID, forge.MergeChangeOptions{}),
				"merge change",
			)
		},

		CloseChange: func(t *testing.T, repo forge.Repository, changeID forge.ChangeID) {
			require.NoError(t,
				giteaforge.CloseChange(t.Context(), repo.(*giteaforge.Repository), changeID),
				"close change",
			)
		},

		SetChangeChecksState: func(
			t *testing.T,
			httpClient *http.Client,
			_ forge.Repository,
			_ forge.ChangeID,
			headHash git.Hash,
			state forge.ChecksState,
		) {
			gc := newTestGiteaClient(t, cfg, httpClient)
			require.NoError(t,
				giteaforge.CommitStatusCreate(
					t.Context(), gc,
					cfg.Owner, cfg.Repo,
					headHash.String(),
					state,
				),
				"set checks state",
			)
		},

		SetCommentsPageSize: func(_ testing.TB, n int) {
			giteaforge.SetListChangeCommentsPageSize(n)
		},

		Reviewers: []string{cfg.Reviewer},
		Assignees: []string{cfg.Assignee},

		// Gitea issue comments have no thread-resolution concept.
		SkipCommentCounts: true,

		// Gitea returns short (7-char) commit hashes in some API responses.
		ShortHeadHash: true,

		// Our ListChangeTemplates reads individual files, not directories.
		// The integration test requires a directory-style template path.
		SkipTemplates: true,

		// Gitea requires labels to be pre-created on the repository.
		// Our resolveLabels skips unknown label names rather than creating
		// them. Labels can be tested with pre-created fixtures.
		SkipLabels: true,
	})
}

// gitAuthURL returns an HTTP URL with embedded token credentials for git push.
// The token is used as both username and password per Gitea's HTTP auth.
func gitAuthURL(baseURL, owner, repo, token string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return baseURL + "/" + owner + "/" + repo
	}
	u.User = url.UserPassword(token, "")
	u.Path += "/" + owner + "/" + repo
	return u.String()
}

// Compile-time check: ensure exported types are usable from _test package.
var _ forge.Forge = (*giteaforge.Forge)(nil)
