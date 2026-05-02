package gitlab

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	gatewaygitlab "go.abhg.dev/gs/internal/gateway/gitlab"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestRepository_SubmitChange_fromPushRepository(t *testing.T) {
	var created bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet &&
			r.URL.EscapedPath() == "/api/v4/projects/test-owner-robot%2Ftest-repo-fork":
			writeGitLabJSON(t, w, http.StatusOK, gatewaygitlab.Project{
				ID: 200,
			})

		case r.Method == http.MethodPost &&
			r.URL.EscapedPath() == "/api/v4/projects/200/merge_requests":
			created = true
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "Stabilize nacelles", body["title"])
			assert.Equal(t, "fork-branch", body["source_branch"])
			assert.Equal(t, "main", body["target_branch"])
			assert.Equal(t, float64(100), body["target_project_id"])
			assert.NotContains(t, body, "source_project_id")

			writeGitLabJSON(t, w, http.StatusCreated, gatewaygitlab.MergeRequest{
				BasicMergeRequest: gatewaygitlab.BasicMergeRequest{
					IID:    55,
					WebURL: "https://gitlab.example.com/test-owner/test-repo/-/merge_requests/55",
				},
			})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client, err := gatewaygitlab.NewClient(
		gatewaygitlab.StaticTokenSource(gatewaygitlab.Token{
			Type:  gatewaygitlab.TokenTypePrivateToken,
			Value: "test-token",
		}),
		&gatewaygitlab.ClientOptions{BaseURL: srv.URL},
	)
	require.NoError(t, err)

	repo := &Repository{
		client: client,
		repoID: 100,
		log:    silogtest.New(t),
	}
	change, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Stabilize nacelles",
		Base:    "main",
		Head:    "fork-branch",
		PushRepository: &RepositoryID{
			url:   "https://gitlab.example.com",
			owner: "test-owner-robot",
			name:  "test-repo-fork",
		},
	})
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, &MR{Number: 55}, change.ID)
	assert.Equal(t,
		"https://gitlab.example.com/test-owner/test-repo/-/merge_requests/55",
		change.URL)
}

func writeGitLabJSON(t *testing.T, w http.ResponseWriter, code int, v any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	require.NoError(t, json.NewEncoder(w).Encode(v))
}
