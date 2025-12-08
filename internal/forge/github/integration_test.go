package github_test

import (
	"context"
	"crypto/rand"
	"net/http"
	"os"
	"testing"

	"github.com/shurcooL/githubv4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/fixturetest"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/forge/github"
	"go.abhg.dev/gs/internal/graphqlutil"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"golang.org/x/oauth2"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

// This file tests basic, end-to-end interactions with the GitHub API
// using recorded fixtures.

var _fixtures = fixturetest.Config{Update: forgetest.Update}

// To avoid looking this up for every test that needs the repo ID,
// we'll just hardcode it here.
var (
	_gitSpiceRepoID = githubv4.ID("R_kgDOJ2BQKg")
	_testRepoID     = githubv4.ID("R_kgDOMVd0xg")
)

// TODO: delete newRecorder when tests have been migrated to forgetest.
func newRecorder(t *testing.T, name string) *recorder.Recorder {
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("To update the test fixtures, run:")
			t.Logf("    GITHUB_TOKEN=$token go test -update -run '^%s$'", t.Name())
		}
	})

	return forgetest.NewHTTPRecorder(t, name)
}

func newGitHubClient(
	httpClient *http.Client,
) *githubv4.Client {
	httpClient.Transport = graphqlutil.WrapTransport(httpClient.Transport)
	return githubv4.NewClient(httpClient)
}

func TestIntegration_Repository(t *testing.T) {
	rec := newRecorder(t, t.Name())
	ghc := newGitHubClient(rec.GetDefaultClient())
	_, err := github.NewRepository(t.Context(), new(github.Forge), "abhinav", "git-spice", silogtest.New(t), ghc, nil)
	require.NoError(t, err)
}

func TestIntegration_Repository_NewChangeMetadata(t *testing.T) {
	rec := newRecorder(t, t.Name())
	ghc := newGitHubClient(rec.GetDefaultClient())
	repo, err := github.NewRepository(t.Context(), new(github.Forge), "abhinav", "git-spice", silogtest.New(t), ghc, _gitSpiceRepoID)
	require.NoError(t, err)

	t.Run("valid", func(t *testing.T) {
		md, err := repo.NewChangeMetadata(t.Context(), &github.PR{Number: 196})
		require.NoError(t, err)

		assert.Equal(t, &github.PR{
			Number: 196,
			GQLID:  "PR_kwDOJ2BQKs5ylEYu",
		}, md.ChangeID())
		assert.Equal(t, "github", md.ForgeID())
	})

	t.Run("invalid", func(t *testing.T) {
		_, err := repo.NewChangeMetadata(t.Context(), &github.PR{Number: 10000})
		require.Error(t, err)
		assert.ErrorContains(t, err, "get pull request ID")
	})
}

func TestIntegration(t *testing.T) {
	t.Cleanup(func() {
		if t.Failed() && !forgetest.Update() {
			t.Logf("To update the test fixtures, run:")
			t.Logf("    GITHUB_TOKEN=$token go test -update -run '^%s$'", t.Name())
		}
	})

	githubForge := github.Forge{
		Log: silogtest.New(t),
	}

	forgetest.RunIntegration(t, forgetest.IntegrationConfig{
		RemoteURL: "https://github.com/abhinav/test-repo",
		Forge:     &githubForge,
		OpenRepository: func(t *testing.T, httpClient *http.Client) forge.Repository {
			githubToken := os.Getenv("GITHUB_TOKEN")
			if githubToken == "" {
				githubToken = "token"
			}

			httpClient.Transport = &oauth2.Transport{
				Base: httpClient.Transport,
				Source: oauth2.StaticTokenSource(&oauth2.Token{
					AccessToken: githubToken,
				}),
			}

			ghc := newGitHubClient(httpClient)
			repo, err := github.NewRepository(
				t.Context(), &githubForge, "abhinav", "test-repo",
				silogtest.New(t), ghc, _testRepoID,
			)
			require.NoError(t, err)
			return repo
		},
		MergeChange: func(t *testing.T, repo forge.Repository, change forge.ChangeID) {
			require.NoError(t, github.MergeChange(t.Context(), repo.(*github.Repository), change.(*github.PR)))
		},
		CloseChange: func(t *testing.T, repo forge.Repository, change forge.ChangeID) {
			require.NoError(t, github.CloseChange(t.Context(), repo.(*github.Repository), change.(*github.PR)))
		},
		SetCommentsPageSize: github.SetListChangeCommentsPageSize,
		Reviewers:           []string{"abhinav-robot"},
		Assignees:           []string{"abhinav-robot", "abhinav"},
	})
}

func TestIntegration_Repository_LabelCreateDelete(t *testing.T) {
	label := fixturetest.New(_fixtures, "label1", func() string { return randomString(8) }).Get(t)

	rec := newRecorder(t, t.Name())
	ghc := newGitHubClient(rec.GetDefaultClient())
	repo, err := github.NewRepository(
		t.Context(), new(github.Forge), "abhinav", "test-repo", silogtest.New(t), ghc, _testRepoID,
	)
	require.NoError(t, err)

	t.Run("DoesNotExist", func(t *testing.T) {
		_, err := repo.LabelID(t.Context(), label)
		require.Error(t, err, "expected error for non-existent label")
		assert.ErrorIs(t, err, github.ErrLabelNotFound)
	})

	id, err := repo.CreateLabel(t.Context(), label)
	require.NoError(t, err, "could not create label")
	t.Cleanup(func() {
		t.Logf("Deleting label: %s", label)
		ctx := context.WithoutCancel(t.Context())
		assert.NoError(t,
			repo.DeleteLabel(ctx, label), "could not delete label")
	})

	t.Run("LabelID", func(t *testing.T) {
		gotID, err := repo.LabelID(t.Context(), label)
		require.NoError(t, err, "could not get label ID")
		assert.Equal(t, id, gotID, "label ID does not match")
	})

	t.Run("createIsIdempotent", func(t *testing.T) {
		newID, err := repo.CreateLabel(t.Context(), label)
		require.NoError(t, err, "could not create label again")

		assert.Equal(t, id, newID, "label ID should be the same on idempotent create")
	})
}

func TestIntegration_Repository_notFoundError(t *testing.T) {
	ctx := t.Context()
	rec := newRecorder(t, t.Name())
	client := rec.GetDefaultClient()
	client.Transport = graphqlutil.WrapTransport(client.Transport)
	ghc := newGitHubClient(client)
	_, err := github.NewRepository(ctx, new(github.Forge), "abhinav", "does-not-exist-repo", silogtest.New(t), ghc, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, graphqlutil.ErrNotFound)

	var gqlError *graphqlutil.Error
	if assert.ErrorAs(t, err, &gqlError) {
		assert.Equal(t, "NOT_FOUND", gqlError.Type)
		assert.Equal(t, []any{"repository"}, gqlError.Path)
		assert.Contains(t, gqlError.Message, "abhinav/does-not-exist-repo")
	}
}

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
