package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shurcooL/githubv4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestRepository_SubmitChange_fromPushRepository(t *testing.T) {
	var created bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)

		var body struct {
			Query     string `json:"query"`
			Variables struct {
				Owner string         `json:"owner"`
				Repo  string         `json:"repo"`
				Input map[string]any `json:"input"`
			} `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))

		if strings.Contains(body.Query, "repository(owner: $owner, name: $repo)") {
			assert.Equal(t, "test-owner-robot", body.Variables.Owner)
			assert.Equal(t, "test-repo", body.Variables.Repo)

			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"repository": map[string]any{
						"id": "pushRepoID",
					},
				},
			}))
			return
		}

		input := body.Variables.Input
		assert.Equal(t, "repoID", input["repositoryId"])
		assert.Equal(t, "main", input["baseRefName"])
		assert.Equal(t, "test-owner-robot:fork-branch", input["headRefName"])
		assert.Equal(t, "Stabilize nacelles", input["title"])
		assert.Equal(t, "pushRepoID", input["headRepositoryId"])
		created = true

		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"createPullRequest": map[string]any{
					"pullRequest": map[string]any{
						"id":     "prID",
						"number": 55,
						"url":    "https://github.com/test-owner/test-repo/pull/55",
					},
				},
			},
		}))
	}))
	defer srv.Close()

	repo, err := newRepository(
		t.Context(), new(Forge),
		"test-owner", "test-repo",
		silogtest.New(t),
		githubv4.NewEnterpriseClient(srv.URL, nil),
		"repoID",
	)
	require.NoError(t, err)

	change, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Stabilize nacelles",
		Base:    "main",
		Head:    "fork-branch",
		PushRepository: &RepositoryID{
			url:   "https://github.com",
			owner: "test-owner-robot",
			name:  "test-repo",
		},
	})
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, &PR{Number: 55, GQLID: githubv4.ID("prID")}, change.ID)
	assert.Equal(t,
		"https://github.com/test-owner/test-repo/pull/55",
		change.URL)
}
