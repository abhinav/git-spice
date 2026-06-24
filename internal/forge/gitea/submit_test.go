package gitea

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestRepository_SubmitChange(t *testing.T) {
	var reviewersCalled bool
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/captain/warp-core/pulls":
			assert.Equal(t, http.MethodPost, r.Method)
			// Reviewers are NOT in the PR body; they go via the dedicated endpoint.
			assertJSONBody(t, r, `{
				"title":"Stabilize nacelles",
				"body":"Replace failing plasma injector.",
				"head":"scotty/fix",
				"base":"main",
				"assignees":["scotty"]
			}`)
			writeJSON(t, w, http.StatusCreated, giteagw.PullRequest{
				Number:  42,
				HTMLURL: "https://gitea.example.com/captain/warp-core/pulls/42",
			})
		case "/api/v1/repos/captain/warp-core/pulls/42/requested_reviewers":
			assert.Equal(t, http.MethodPost, r.Method)
			assertJSONBody(t, r, `{"reviewers":["spock"]}`)
			reviewersCalled = true
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	result, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject:   "Stabilize nacelles",
		Body:      "Replace failing plasma injector.",
		Head:      "scotty/fix",
		Base:      "main",
		Assignees: []string{"scotty"},
		Reviewers: []string{"spock"},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(42), result.ID.(*PR).Number)
	assert.Equal(t, "https://gitea.example.com/captain/warp-core/pulls/42", result.URL)
	assert.True(t, reviewersCalled, "reviewer endpoint should have been called")
}

func TestRepository_SubmitChange_draft(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/captain/warp-core/pulls":
			// Draft is indicated by "WIP:" title prefix, not the draft field.
			assertJSONBody(t, r, `{
				"title":"WIP: WIP feature",
				"head":"scotty/wip",
				"base":"main"
			}`)
			writeJSON(t, w, http.StatusCreated, giteagw.PullRequest{
				Number:  43,
				Draft:   true,
				HTMLURL: "https://gitea.example.com/captain/warp-core/pulls/43",
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	result, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "WIP feature",
		Head:    "scotty/wip",
		Base:    "main",
		Draft:   true,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(43), result.ID.(*PR).Number)
}

func TestRepository_SubmitChange_withLabels(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/captain/warp-core/labels":
			writeJSON(t, w, http.StatusOK, []*giteagw.Label{
				{ID: 10, Name: "engineering"},
				{ID: 11, Name: "priority-1"},
			})
		case "/api/v1/repos/captain/warp-core/pulls":
			assertJSONBody(t, r, `{
				"title":"Fix nacelles",
				"head":"fix",
				"base":"main",
				"labels":[10,11]
			}`)
			writeJSON(t, w, http.StatusCreated, giteagw.PullRequest{
				Number:  44,
				HTMLURL: "https://gitea.example.com/captain/warp-core/pulls/44",
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	result, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Fix nacelles",
		Head:    "fix",
		Base:    "main",
		Labels:  []string{"engineering", "priority-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(44), result.ID.(*PR).Number)
}

// newTestServer wraps an HTTP handler. The repo-get and user endpoints that
// newRepository always calls are served automatically; the inner handler
// receives all other requests.
func newTestServer(t *testing.T, inner http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/captain/warp-core":
			writeJSON(t, w, http.StatusOK, giteagw.Repository{
				ID:       1,
				FullName: "captain/warp-core",
			})
		case "/api/v1/user":
			writeJSON(t, w, http.StatusOK, giteagw.User{ID: 1, Login: "scotty"})
		default:
			inner(w, r)
		}
	}))
}

func newTestRepo(t *testing.T, srv *httptest.Server) *Repository {
	t.Helper()

	client, err := giteagw.NewClient(
		giteagw.StaticTokenSource(giteagw.Token{
			Type:  giteagw.TokenTypeToken,
			Value: "test-token",
		}),
		&giteagw.ClientOptions{BaseURL: srv.URL},
	)
	require.NoError(t, err)

	f := &Forge{
		Options: Options{URL: srv.URL},
		Log:     silogtest.New(t),
	}

	repo, err := newRepository(t.Context(), f, "captain", "warp-core", silogtest.New(t), client)
	require.NoError(t, err)
	return repo
}
