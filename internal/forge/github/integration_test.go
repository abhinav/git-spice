package github_test

import (
	"bytes"
	"context"
	"flag"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shurcooL/githubv4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/github"
	"go.abhg.dev/gs/internal/logtest"
	"golang.org/x/oauth2"
	"gopkg.in/dnaeon/go-vcr.v3/cassette"
	"gopkg.in/dnaeon/go-vcr.v3/recorder"
)

// This file tests basic, end-to-end interactions with the GitHub API
// using recorded fixtures.

var _update = flag.Bool("update", false, "update test fixtures")

func newRecorder(t *testing.T, name string) *recorder.Recorder {
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("To update the test fixtures, run:")
			t.Logf("    GITHUB_TOKEN=$token go test -update -run '^%s$'", t.Name())
		}
	})

	var (
		realTransport    http.RoundTripper
		afterCaptureHook recorder.HookFunc
	)
	mode := recorder.ModeReplayOnly
	if *_update {
		mode = recorder.ModeRecordOnly
		githubToken := os.Getenv("GITHUB_TOKEN")
		require.NotEmpty(t, githubToken,
			"$GITHUB_TOKEN must be set in record mode")

		realTransport = &oauth2.Transport{
			Source: oauth2.StaticTokenSource(&oauth2.Token{
				AccessToken: githubToken,
			}),
		}

		// Because the oauth transport is the inner transport,
		// the recorder will never see the Authorization header.
		// But for extra paranoia, we'll strip all but a handful
		// of headers from the request and response before saving.
		afterCaptureHook = func(i *cassette.Interaction) error {
			allHeaders := make(http.Header)
			maps.Copy(allHeaders, i.Request.Headers)
			maps.Copy(allHeaders, i.Response.Headers)

			var toRemove []string
			for k := range allHeaders {
				switch strings.ToLower(k) {
				case "content-type", "content-length", "user-agent":
					// ok
				default:
					toRemove = append(toRemove, k)
				}
			}

			for _, k := range toRemove {
				delete(i.Request.Headers, k)
				delete(i.Response.Headers, k)
			}

			return nil
		}
	}

	rec, err := recorder.NewWithOptions(&recorder.Options{
		CassetteName:       filepath.Join("testdata", "fixtures", name),
		Mode:               mode,
		RealTransport:      realTransport,
		SkipRequestLatency: true, // don't go slow
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, rec.Stop())
	})

	// GraphQL requests will all have the same method and URL.
	// We'll need to match the body instead.
	rec.SetMatcher(func(r *http.Request, i cassette.Request) bool {
		if r.Body == nil || r.Body == http.NoBody {
			return cassette.DefaultMatcher(r, i)
		}

		reqBody, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.NoError(t, r.Body.Close())

		r.Body = io.NopCloser(bytes.NewBuffer(reqBody))
		return r.Method == i.Method &&
			r.URL.String() == i.URL &&
			string(reqBody) == i.Body
	})

	if afterCaptureHook != nil {
		rec.AddHook(afterCaptureHook, recorder.AfterCaptureHook)
	}

	return rec
}

// To avoid looking this up for every test that needs a repo.
var _gitSpiceRepoID = githubv4.ID("R_kgDOJ2BQKg")

func TestIntegration_Repository(t *testing.T) {
	ctx := context.Background()
	rec := newRecorder(t, t.Name())
	ghc := githubv4.NewClient(rec.GetDefaultClient())
	_, err := github.NewRepository(ctx, "abhinav", "git-spice", logtest.New(t), ghc, nil)
	require.NoError(t, err)
}

func TestIntegration_Repository_FindChangeByID(t *testing.T) {
	ctx := context.Background()
	rec := newRecorder(t, t.Name())
	ghc := githubv4.NewClient(rec.GetDefaultClient())
	repo, err := github.NewRepository(ctx, "abhinav", "git-spice", logtest.New(t), ghc, _gitSpiceRepoID)
	require.NoError(t, err)

	t.Run("found", func(t *testing.T) {
		change, err := repo.FindChangeByID(ctx, 141)
		require.NoError(t, err)

		assert.Equal(t, &forge.FindChangeItem{
			ID:       141,
			URL:      "https://github.com/abhinav/git-spice/pull/141",
			Subject:  "branch submit: Heal from external PR submissions",
			State:    forge.ChangeMerged,
			BaseName: "main",
			HeadHash: "df0289d83ffae816105947875db01c992224913d",
			Draft:    false,
		}, change)
	})

	t.Run("not-found", func(t *testing.T) {
		_, err := repo.FindChangeByID(ctx, 999)
		require.Error(t, err)
		assert.ErrorContains(t, err, "Could not resolve")
	})
}

func TestIntegration_Repository_FindChangesByBranch(t *testing.T) {
	ctx := context.Background()
	rec := newRecorder(t, t.Name())
	ghc := githubv4.NewClient(rec.GetDefaultClient())
	repo, err := github.NewRepository(ctx, "abhinav", "git-spice", logtest.New(t), ghc, _gitSpiceRepoID)
	require.NoError(t, err)

	t.Run("found", func(t *testing.T) {
		changes, err := repo.FindChangesByBranch(ctx, "gh-graphql", forge.FindChangesOptions{})
		require.NoError(t, err)
		assert.Equal(t, []*forge.FindChangeItem{
			{
				ID:       144,
				URL:      "https://github.com/abhinav/git-spice/pull/144",
				State:    forge.ChangeMerged,
				Subject:  "GitHub: Use GraphQL API",
				BaseName: "main",
				HeadHash: "5d74cecfe3cb066044d129232229e07f5d04e194",
				Draft:    false,
			},
		}, changes)
	})

	t.Run("not-found", func(t *testing.T) {
		changes, err := repo.FindChangesByBranch(ctx, "does-not-exist", forge.FindChangesOptions{})
		require.NoError(t, err)
		assert.Empty(t, changes)
	})
}

func TestIntegration_Repository_IsMerged(t *testing.T) {
	ctx := context.Background()
	rec := newRecorder(t, t.Name())
	ghc := githubv4.NewClient(rec.GetDefaultClient())
	repo, err := github.NewRepository(ctx, "abhinav", "git-spice", logtest.New(t), ghc, _gitSpiceRepoID)
	require.NoError(t, err)

	t.Run("false", func(t *testing.T) {
		ok, err := repo.ChangeIsMerged(ctx, 144)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("true", func(t *testing.T) {
		ok, err := repo.ChangeIsMerged(ctx, 141)
		require.NoError(t, err)
		assert.True(t, ok)
	})
}

func TestIntegration_Repository_ListChangeTemplates(t *testing.T) {
	ctx := context.Background()

	t.Run("absent", func(t *testing.T) {
		rec := newRecorder(t, t.Name())
		ghc := githubv4.NewClient(rec.GetDefaultClient())
		repo, err := github.NewRepository(ctx, "abhinav", "git-spice", logtest.New(t), ghc, _gitSpiceRepoID)
		require.NoError(t, err)

		templates, err := repo.ListChangeTemplates(ctx)
		require.NoError(t, err)
		assert.Empty(t, templates)
	})

	t.Run("present", func(t *testing.T) {
		rec := newRecorder(t, t.Name())
		ghc := githubv4.NewClient(rec.GetDefaultClient())
		repo, err := github.NewRepository(ctx, "golang", "go", logtest.New(t), ghc, nil)
		require.NoError(t, err)

		templates, err := repo.ListChangeTemplates(ctx)
		require.NoError(t, err)
		require.Len(t, templates, 1)

		template := templates[0]
		assert.Equal(t, "PULL_REQUEST_TEMPLATE", template.Filename)
		assert.NotEmpty(t, template.Body)
	})
}
