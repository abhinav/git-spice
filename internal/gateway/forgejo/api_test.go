package forgejo

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_RepositoryGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/repos/owner/repo", r.URL.Path)
		assert.Empty(t, r.URL.RawQuery)
		assert.Equal(t, "git-spice", r.Header.Get("User-Agent"))
		writeJSON(t, w, http.StatusOK, Repository{
			ID:       42,
			FullName: "owner/repo",
			Permissions: &Permission{
				Pull: true,
				Push: true,
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	repo, _, err := client.RepositoryGet(t.Context(), "owner", "repo")
	require.NoError(t, err)
	assert.Equal(t, int64(42), repo.ID)
	require.NotNil(t, repo.Permissions)
	assert.True(t, repo.Permissions.Push)
}

func TestClient_PullRequestCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/repos/owner/repo/pulls", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "token test-token", r.Header.Get("Authorization"))
		assertJSONBody(t, r, `{
			"title":"Update feature branch",
			"body":"Update release notes.",
			"head":"contributor/feature",
			"base":"main",
			"assignees":["reviewer"],
			"labels":[11,12],
			"draft":true
		}`)
		writeJSON(t, w, http.StatusCreated, PullRequest{
			Index:   55,
			HTMLURL: "https://forgejo.example.com/owner/repo/pulls/55",
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	pr, _, err := client.PullRequestCreate(
		t.Context(),
		"owner",
		"repo",
		&CreatePullRequestOption{
			Title:     "Update feature branch",
			Body:      "Update release notes.",
			Head:      "contributor/feature",
			Base:      "main",
			Assignees: []string{"reviewer"},
			Labels:    []int64{11, 12},
			Draft:     true,
		},
	)
	require.NoError(t, err)
	assert.Equal(t, int64(55), pr.Index)
	assert.Equal(
		t,
		"https://forgejo.example.com/owner/repo/pulls/55",
		pr.HTMLURL,
	)
}

func TestClient_PullRequestList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/repos/owner/repo/pulls", r.URL.Path)
		assert.Equal(t, "open", r.URL.Query().Get("state"))
		assert.Equal(t, "recentupdate", r.URL.Query().Get("sort"))
		assert.Equal(t, "20", r.URL.Query().Get("limit"))
		writeJSON(t, w, http.StatusOK, []*PullRequest{
			{Index: 55, Title: "One"},
			{Index: 56, Title: "Two"},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	pullRequests, _, err := client.PullRequestList(
		t.Context(),
		"owner",
		"repo",
		&PullRequestListOptions{
			ListOptions: ListOptions{Limit: 20},
			State:       "open",
			Sort:        "recentupdate",
		},
	)
	require.NoError(t, err)
	require.Len(t, pullRequests, 2)
	assert.Equal(t, int64(56), pullRequests[1].Index)
}

func TestClient_PullRequestEdit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "/api/v1/repos/owner/repo/pulls/55", r.URL.Path)
		assertJSONBody(t, r, `{
			"title":"Draft: Update feature branch",
			"base":"release",
			"assignees":["reviewer"],
			"labels":[11],
			"state":"closed"
		}`)
		writeJSON(t, w, http.StatusOK, PullRequest{
			Index: 55,
			Title: "Draft: Update feature branch",
			Base:  &PRBranchInfo{Ref: "release"},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	pr, _, err := client.PullRequestEdit(
		t.Context(),
		"owner",
		"repo",
		55,
		&EditPullRequestOption{
			Title:     new("Draft: Update feature branch"),
			Base:      new("release"),
			Assignees: &[]string{"reviewer"},
			Labels:    &[]int64{11},
			State:     new("closed"),
		},
	)
	require.NoError(t, err)
	assert.Equal(t, "Draft: Update feature branch", pr.Title)
	require.NotNil(t, pr.Base)
	assert.Equal(t, "release", pr.Base.Ref)
}

func TestClient_PullRequestIsMerged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/repos/owner/repo/pulls/55/merge", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	resp, err := client.PullRequestIsMerged(t.Context(), "owner", "repo", 55)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestClient_IssueCommentCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/repos/owner/repo/issues/55/comments", r.URL.Path)
		assertJSONBody(t, r, `{"body":"Review notes ready."}`)
		writeJSON(t, w, http.StatusCreated, Comment{
			ID:   99,
			Body: "Review notes ready.",
			User: &User{Login: "contributor"},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	comment, _, err := client.IssueCommentCreate(
		t.Context(),
		"owner",
		"repo",
		55,
		&CreateIssueCommentOption{Body: "Review notes ready."},
	)
	require.NoError(t, err)
	assert.Equal(t, int64(99), comment.ID)
	require.NotNil(t, comment.User)
	assert.Equal(t, "contributor", comment.User.Login)
}

func TestClient_CommitStatusCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/repos/owner/repo/statuses/abc123", r.URL.Path)
		assertJSONBody(t, r, `{
			"state":"success",
			"target_url":"https://ci.example.com/build/1",
			"description":"Build passed",
			"context":"ci/unit"
		}`)
		writeJSON(t, w, http.StatusCreated, CommitStatus{
			ID:    7,
			State: CommitStatusSuccess,
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	status, _, err := client.CommitStatusCreate(
		t.Context(),
		"owner",
		"repo",
		"abc123",
		&CreateStatusOption{
			State:       CommitStatusSuccess,
			TargetURL:   "https://ci.example.com/build/1",
			Description: "Build passed",
			Context:     "ci/unit",
		},
	)
	require.NoError(t, err)
	assert.Equal(t, int64(7), status.ID)
	assert.Equal(t, CommitStatusSuccess, status.State)
}

func TestClient_UserSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/users/search", r.URL.Path)
		assert.Equal(t, "reviewer", r.URL.Query().Get("q"))
		assert.Equal(t, "1", r.URL.Query().Get("page"))
		writeJSON(t, w, http.StatusOK, UserSearchResults{
			OK: true,
			Data: []*User{
				{ID: 9, Login: "reviewer"},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	users, _, err := client.UserSearch(t.Context(), &UserSearchOptions{
		ListOptions: ListOptions{Page: 1},
		Query:       "reviewer",
	})
	require.NoError(t, err)
	require.Len(t, users.Data, 1)
	assert.Equal(t, "reviewer", users.Data[0].Login)
}
