package gitea

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_UserCurrent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/user", r.URL.Path)
		assert.Equal(t, "git-spice", r.Header.Get("User-Agent"))
		writeJSON(t, w, http.StatusOK, User{
			ID:    7,
			Login: "spock",
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	user, _, err := client.UserCurrent(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int64(7), user.ID)
	assert.Equal(t, "spock", user.Login)
}

func TestClient_PullCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/repos/captain/warp-core/pulls", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assertJSONBody(t, r, `{
			"title":"Stabilize nacelles",
			"body":"Replace failing plasma injector.",
			"head":"scotty/fix",
			"base":"main",
			"labels":[1,2],
			"assignees":["scotty"],
			"reviewers":["spock","uhura"]
		}`)
		writeJSON(t, w, http.StatusCreated, PullRequest{
			Number:  42,
			HTMLURL: "https://gitea.example.com/captain/warp-core/pulls/42",
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	pr, _, err := client.PullCreate(t.Context(), "captain", "warp-core", &CreatePullRequestOption{
		Title:     "Stabilize nacelles",
		Body:      "Replace failing plasma injector.",
		Head:      "scotty/fix",
		Base:      "main",
		Labels:    []int64{1, 2},
		Assignees: []string{"scotty"},
		Reviewers: []string{"spock", "uhura"},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(42), pr.Number)
	assert.Equal(t, "https://gitea.example.com/captain/warp-core/pulls/42", pr.HTMLURL)
}

func TestClient_PullCreate_draft(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertJSONBody(t, r, `{
			"title":"WIP: Stabilize nacelles",
			"head":"scotty/fix",
			"base":"main",
			"draft":true
		}`)
		writeJSON(t, w, http.StatusCreated, PullRequest{Number: 43, Draft: true})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	pr, _, err := client.PullCreate(t.Context(), "captain", "warp-core", &CreatePullRequestOption{
		Title: "WIP: Stabilize nacelles",
		Head:  "scotty/fix",
		Base:  "main",
		Draft: true,
	})
	require.NoError(t, err)
	assert.True(t, pr.Draft)
}

func TestClient_PullGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/repos/captain/warp-core/pulls/42", r.URL.Path)
		writeJSON(t, w, http.StatusOK, PullRequest{
			Number: 42,
			Title:  "Stabilize nacelles",
			State:  "open",
			Head:   &PRBranch{Ref: "scotty/fix", Sha: "abc123"},
			Base:   &PRBranch{Ref: "main"},
			Labels: []*Label{{ID: 1, Name: "engineering"}},
			RequestedReviewers: []*User{
				{ID: 9, Login: "spock"},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	pr, _, err := client.PullGet(t.Context(), "captain", "warp-core", 42)
	require.NoError(t, err)
	assert.Equal(t, int64(42), pr.Number)
	assert.Equal(t, "open", pr.State)
	require.NotNil(t, pr.Head)
	assert.Equal(t, "abc123", pr.Head.Sha)
	require.Len(t, pr.Labels, 1)
	assert.Equal(t, "engineering", pr.Labels[0].Name)
	require.Len(t, pr.RequestedReviewers, 1)
	assert.Equal(t, "spock", pr.RequestedReviewers[0].Login)
}

func TestClient_PullEdit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "/api/v1/repos/captain/warp-core/pulls/42", r.URL.Path)
		assertJSONBody(t, r, `{
			"title":"Draft: Stabilize nacelles",
			"base":"release",
			"draft":true,
			"labels":[1,2],
			"assignees":["scotty"],
			"reviewers":["spock"]
		}`)
		writeJSON(t, w, http.StatusOK, PullRequest{
			Number: 42,
			Title:  "Draft: Stabilize nacelles",
			Base:   &PRBranch{Ref: "release"},
			Draft:  true,
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	pr, _, err := client.PullEdit(t.Context(), "captain", "warp-core", 42, &EditPullRequestOption{
		Title:     new("Draft: Stabilize nacelles"),
		Base:      new("release"),
		Draft:     new(true),
		Labels:    []int64{1, 2},
		Assignees: []string{"scotty"},
		Reviewers: []string{"spock"},
	})
	require.NoError(t, err)
	assert.Equal(t, "release", pr.Base.Ref)
	assert.True(t, pr.Draft)
}

func TestClient_PullList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/repos/captain/warp-core/pulls", r.URL.Path)
		assert.Equal(t, "open", r.URL.Query().Get("state"))
		assert.Equal(t, "scotty/fix", r.URL.Query().Get("head"))
		assert.Equal(t, "10", r.URL.Query().Get("limit"))
		w.Header().Set("X-Page", "1")
		w.Header().Set("X-Total-Pages", "1")
		writeJSON(t, w, http.StatusOK, []*PullRequest{
			{Number: 42, Title: "Stabilize nacelles"},
			{Number: 43, Title: "Refit shields"},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	prs, resp, err := client.PullList(t.Context(), "captain", "warp-core", &ListPullRequestsOptions{
		ListOptions: ListOptions{Limit: 10},
		State:       "open",
		Head:        "scotty/fix",
	})
	require.NoError(t, err)
	require.Len(t, prs, 2)
	assert.Equal(t, int64(43), prs[1].Number)
	assert.Equal(t, 1, resp.CurrentPage)
}

func TestClient_PullMerge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/repos/captain/warp-core/pulls/42/merge", r.URL.Path)
		assertJSONBody(t, r, `{"Do":"squash","head_commit_id":"abc123"}`)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	_, err := client.PullMerge(t.Context(), "captain", "warp-core", 42, &MergePullRequestOption{
		Do:           "squash",
		HeadCommitID: "abc123",
	})
	require.NoError(t, err)
}

func TestClient_CommentCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/repos/captain/warp-core/issues/42/comments", r.URL.Path)
		assertJSONBody(t, r, `{"body":"Recalibrated deflector array."}`)
		writeJSON(t, w, http.StatusCreated, Comment{
			ID:   88,
			Body: "Recalibrated deflector array.",
			User: &User{ID: 1, Login: "scotty"},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	comment, _, err := client.CommentCreate(t.Context(), "captain", "warp-core", 42,
		"Recalibrated deflector array.")
	require.NoError(t, err)
	assert.Equal(t, int64(88), comment.ID)
}

func TestClient_CommentEdit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "/api/v1/repos/captain/warp-core/issues/comments/88", r.URL.Path)
		assertJSONBody(t, r, `{"body":"Updated calibration."}`)
		writeJSON(t, w, http.StatusOK, Comment{ID: 88, Body: "Updated calibration."})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	comment, _, err := client.CommentEdit(t.Context(), "captain", "warp-core", 88,
		"Updated calibration.")
	require.NoError(t, err)
	assert.Equal(t, "Updated calibration.", comment.Body)
}

func TestClient_CommentDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/api/v1/repos/captain/warp-core/issues/comments/88", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	resp, err := client.CommentDelete(t.Context(), "captain", "warp-core", 88)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestClient_CommentList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/repos/captain/warp-core/issues/42/comments", r.URL.Path)
		assert.Equal(t, "20", r.URL.Query().Get("limit"))
		assert.Equal(t, "2", r.URL.Query().Get("page"))
		w.Header().Set("X-Page", "2")
		w.Header().Set("X-Next-Page", "3")
		w.Header().Set("X-Total-Pages", "4")
		writeJSON(t, w, http.StatusOK, []*Comment{
			{ID: 88, Body: "alpha", User: &User{ID: 1, Login: "scotty"}},
			{ID: 89, Body: "beta", User: &User{ID: 2, Login: "spock"}},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	comments, resp, err := client.CommentList(t.Context(), "captain", "warp-core", 42,
		&ListIssueCommentsOptions{
			ListOptions: ListOptions{Page: 2, Limit: 20},
		},
	)
	require.NoError(t, err)
	require.Len(t, comments, 2)
	assert.Equal(t, "beta", comments[1].Body)
	assert.Equal(t, 2, resp.CurrentPage)
	assert.Equal(t, 3, resp.NextPage)
}

func TestClient_CommitStatusCombined(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/repos/captain/warp-core/commits/abc123/status", r.URL.Path)
		writeJSON(t, w, http.StatusOK, CombinedStatus{State: CommitStatusSuccess})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	status, _, err := client.CommitStatusCombined(t.Context(), "captain", "warp-core", "abc123")
	require.NoError(t, err)
	assert.Equal(t, CommitStatusSuccess, status.State)
}

func TestClient_CommitStatusList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/repos/captain/warp-core/commits/abc123/statuses", r.URL.Path)
		assert.Equal(t, "2", r.URL.Query().Get("page"))
		assert.Equal(t, "20", r.URL.Query().Get("limit"))
		writeJSON(t, w, http.StatusOK, []*CommitStatus{
			{ID: 1, State: CommitStatusSuccess, Context: "ci/test"},
			{ID: 2, State: CommitStatusPending, Context: "ci/lint"},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	statuses, _, err := client.CommitStatusList(
		t.Context(),
		"captain",
		"warp-core",
		"abc123",
		&ListCommitStatusOptions{
			ListOptions: ListOptions{Page: 2, Limit: 20},
		},
	)
	require.NoError(t, err)
	require.Len(t, statuses, 2)
	assert.Equal(t, "ci/lint", statuses[1].Context)
}

func TestClient_LabelList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/repos/captain/warp-core/labels", r.URL.Path)
		assert.Equal(t, "50", r.URL.Query().Get("limit"))
		writeJSON(t, w, http.StatusOK, []*Label{
			{ID: 1, Name: "engineering"},
			{ID: 2, Name: "priority-1"},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	labels, _, err := client.LabelList(t.Context(), "captain", "warp-core",
		&ListLabelsOptions{ListOptions: ListOptions{Limit: 50}},
	)
	require.NoError(t, err)
	require.Len(t, labels, 2)
	assert.Equal(t, "priority-1", labels[1].Name)
}

func TestClient_FileContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/repos/captain/warp-core/contents/.gitea/PULL_REQUEST_TEMPLATE.md", r.URL.Path)
		writeJSON(t, w, http.StatusOK, FileContentResponse{
			Name:     "PULL_REQUEST_TEMPLATE.md",
			Encoding: "base64",
			Content:  "IyBDaGVja2xpc3Q=", // base64("# Checklist")
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	f, _, err := client.FileContent(t.Context(), "captain", "warp-core",
		".gitea/PULL_REQUEST_TEMPLATE.md")
	require.NoError(t, err)
	assert.Equal(t, "PULL_REQUEST_TEMPLATE.md", f.Name)
	assert.Equal(t, "IyBDaGVja2xpc3Q=", f.Content)
}

func TestClient_PullList_paginated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "", "1":
			w.Header().Set("X-Page", "1")
			w.Header().Set("X-Next-Page", "2")
			w.Header().Set("X-Total-Pages", "2")
			writeJSON(t, w, http.StatusOK, []*PullRequest{
				{Number: 1, Title: "One"},
				{Number: 2, Title: "Two"},
			})
		case "2":
			w.Header().Set("X-Page", "2")
			w.Header().Set("X-Next-Page", "")
			w.Header().Set("X-Total-Pages", "2")
			writeJSON(t, w, http.StatusOK, []*PullRequest{
				{Number: 3, Title: "Three"},
			})
		default:
			t.Fatalf("unexpected page: %q", r.URL.Query().Get("page"))
		}
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	var all []*PullRequest
	opts := &ListPullRequestsOptions{
		ListOptions: ListOptions{Limit: 2},
	}
	for {
		page, resp, err := client.PullList(t.Context(), "captain", "warp-core", opts)
		require.NoError(t, err)
		all = append(all, page...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = int64(resp.NextPage)
	}

	require.Len(t, all, 3)
	assert.Equal(t, int64(1), all[0].Number)
	assert.Equal(t, int64(3), all[2].Number)
}
