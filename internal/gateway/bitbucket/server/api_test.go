package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPullRequestCreate(t *testing.T) {
	var (
		gotPath   string
		gotMethod string
		gotBody   PullRequestCreateRequest
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))

		writeJSON(t, w, http.StatusCreated, map[string]any{
			"id":      42,
			"version": 0,
			"title":   gotBody.Title,
			"links": map[string]any{
				"self": []map[string]any{
					{"href": "https://bitbucket.example.com/projects/ENG/repos/warp-core/pull-requests/42/overview"},
				},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	pr, resp, err := client.PullRequestCreate(t.Context(), "ENG", "warp-core",
		PullRequestCreateRequest{
			Title:       "Refit",
			Description: "desc",
			FromRef: CreateRef{
				ID: "refs/heads/feature",
				Repository: CreateRefRepository{
					Slug:    "warp-core",
					Project: CreateRefProject{Key: "ENG"},
				},
			},
			ToRef: CreateRef{
				ID: "refs/heads/main",
				Repository: CreateRefRepository{
					Slug:    "warp-core",
					Project: CreateRefProject{Key: "ENG"},
				},
			},
			Reviewers: []CreateReviewer{
				{User: CreateReviewerUser{Name: "spock"}},
			},
			Draft: true,
		})
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/rest/api/1.0/projects/ENG/repos/warp-core/pull-requests", gotPath)

	assert.Equal(t, "Refit", gotBody.Title)
	assert.Equal(t, "refs/heads/feature", gotBody.FromRef.ID)
	assert.Equal(t, "refs/heads/main", gotBody.ToRef.ID)
	assert.Equal(t, "warp-core", gotBody.FromRef.Repository.Slug)
	assert.Equal(t, "ENG", gotBody.ToRef.Repository.Project.Key)
	require.Len(t, gotBody.Reviewers, 1)
	assert.Equal(t, "spock", gotBody.Reviewers[0].User.Name)
	assert.True(t, gotBody.Draft)

	assert.Equal(t, int64(42), pr.ID)
	assert.Equal(t,
		"https://bitbucket.example.com/projects/ENG/repos/warp-core/pull-requests/42/overview",
		pr.Links.Self[0].Href)
}

func TestPullRequestCreate_badRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusBadRequest, map[string]any{
			"errors": []map[string]any{
				{
					"message":       `The branch "refs/heads/main" does not exist.`,
					"exceptionName": "com.atlassian.bitbucket.validation.ArgumentValidationException",
				},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	_, _, err := client.PullRequestCreate(t.Context(), "ENG", "warp-core",
		PullRequestCreateRequest{
			Title: "Refit",
			FromRef: CreateRef{
				ID: "refs/heads/feature",
				Repository: CreateRefRepository{
					Slug:    "warp-core",
					Project: CreateRefProject{Key: "ENG"},
				},
			},
			ToRef: CreateRef{
				ID: "refs/heads/main",
				Repository: CreateRefRepository{
					Slug:    "warp-core",
					Project: CreateRefProject{Key: "ENG"},
				},
			},
		})
	require.Error(t, err)

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Len(t, apiErr.Details, 1)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestPullRequestCreate_unrelatedBadRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusBadRequest, map[string]any{
			"errors": []map[string]any{
				{
					"message":       "Only one pull request may be open for a given source and target branch.",
					"exceptionName": "com.atlassian.bitbucket.pull.DuplicatePullRequestException",
				},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	_, _, err := client.PullRequestCreate(t.Context(), "ENG", "warp-core",
		PullRequestCreateRequest{
			Title: "Refit",
			FromRef: CreateRef{
				ID: "refs/heads/feature",
				Repository: CreateRefRepository{
					Slug:    "warp-core",
					Project: CreateRefProject{Key: "ENG"},
				},
			},
			ToRef: CreateRef{
				ID: "refs/heads/main",
				Repository: CreateRefRepository{
					Slug:    "warp-core",
					Project: CreateRefProject{Key: "ENG"},
				},
			},
		})
	require.Error(t, err)

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
}

func TestPullRequestGet(t *testing.T) {
	var (
		gotPath   string
		gotMethod string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		writeJSON(t, w, http.StatusOK, map[string]any{
			"id":          42,
			"version":     7,
			"title":       "Refit",
			"description": "desc",
			"state":       "OPEN",
			"open":        true,
			"fromRef": map[string]any{
				"id":           "refs/heads/feature",
				"displayId":    "feature",
				"latestCommit": "abc123",
			},
			"toRef": map[string]any{
				"id":        "refs/heads/main",
				"displayId": "main",
			},
			"links": map[string]any{
				"self": []map[string]any{
					{"href": "https://bitbucket.example.com/projects/ENG/repos/warp-core/pull-requests/42/overview"},
				},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	pr, resp, err := client.PullRequestGet(t.Context(), "ENG", "warp-core", 42)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, http.MethodGet, gotMethod)
	assert.Equal(t, "/rest/api/1.0/projects/ENG/repos/warp-core/pull-requests/42", gotPath)

	assert.Equal(t, int64(42), pr.ID)
	assert.Equal(t, 7, pr.Version)
	assert.Equal(t, "OPEN", pr.State)
	assert.Equal(t, "abc123", pr.FromRef.LatestCommit)
	assert.Equal(t, "main", pr.ToRef.DisplayID)
	assert.Equal(t,
		"https://bitbucket.example.com/projects/ENG/repos/warp-core/pull-requests/42/overview",
		pr.Links.Self[0].Href)
}

func TestPullRequestGet_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	_, _, err := client.PullRequestGet(t.Context(), "ENG", "warp-core", 99)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestPullRequestList(t *testing.T) {
	var requests []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Query())
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/rest/api/1.0/projects/ENG/repos/warp-core/pull-requests", r.URL.Path)

		switch r.URL.Query().Get("start") {
		case "0":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"values": []map[string]any{
					{"id": 1, "title": "first"},
					{"id": 2, "title": "second"},
				},
				"isLastPage":    false,
				"nextPageStart": 2,
			})
		case "2":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"values": []map[string]any{
					{"id": 3, "title": "third"},
				},
				"isLastPage": true,
			})
		default:
			t.Errorf("unexpected start: %q", r.URL.Query().Get("start"))
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	var ids []int64
	for pr, err := range client.PullRequestList(t.Context(), "ENG", "warp-core",
		PullRequestListRequest{
			At:        "refs/heads/feature",
			Direction: "OUTGOING",
			State:     "OPEN",
		}) {
		require.NoError(t, err)
		ids = append(ids, pr.ID)
	}

	assert.Equal(t, []int64{1, 2, 3}, ids)
	require.Len(t, requests, 2)
	// Query params are forwarded on every page request.
	assert.Equal(t, "refs/heads/feature", requests[0].Get("at"))
	assert.Equal(t, "OUTGOING", requests[0].Get("direction"))
	assert.Equal(t, "OPEN", requests[0].Get("state"))
	assert.Equal(t, "refs/heads/feature", requests[1].Get("at"))
	assert.Equal(t, "OUTGOING", requests[1].Get("direction"))
	assert.Equal(t, "OPEN", requests[1].Get("state"))
}

func TestPullRequestList_omitsEmptyQueryParams(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		writeJSON(t, w, http.StatusOK, map[string]any{
			"values":     []map[string]any{},
			"isLastPage": true,
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	for _, err := range client.PullRequestList(t.Context(), "ENG", "warp-core",
		PullRequestListRequest{State: "ALL"}) {
		require.NoError(t, err)
	}

	assert.Equal(t, "ALL", gotQuery.Get("state"))
	_, hasAt := gotQuery["at"]
	_, hasDirection := gotQuery["direction"]
	assert.False(t, hasAt, "empty At must be omitted")
	assert.False(t, hasDirection, "empty Direction must be omitted")
}

func TestPullRequestUpdate(t *testing.T) {
	var (
		gotPath   string
		gotMethod string
		gotBody   PullRequestUpdateRequest
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))

		writeJSON(t, w, http.StatusOK, map[string]any{
			"id":          42,
			"version":     8,
			"title":       gotBody.Title,
			"description": "updated",
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	description := "updated"
	pr, resp, err := client.PullRequestUpdate(t.Context(), "ENG", "warp-core", 42,
		PullRequestUpdateRequest{
			Version:     7,
			Title:       "Refit again",
			Description: &description,
			Reviewers: []CreateReviewer{
				{User: CreateReviewerUser{Name: "spock"}},
			},
		})
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, http.MethodPut, gotMethod)
	assert.Equal(t, "/rest/api/1.0/projects/ENG/repos/warp-core/pull-requests/42", gotPath)

	// The optimistic-locking version must be carried in the PUT body.
	assert.Equal(t, 7, gotBody.Version)
	assert.Equal(t, "Refit again", gotBody.Title)
	require.NotNil(t, gotBody.Description)
	assert.Equal(t, "updated", *gotBody.Description)
	require.Len(t, gotBody.Reviewers, 1)
	assert.Equal(t, "spock", gotBody.Reviewers[0].User.Name)

	assert.Equal(t, int64(42), pr.ID)
	assert.Equal(t, 8, pr.Version)
}

func TestPullRequestUpdate_omitsUnsetFields(t *testing.T) {
	var raw map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&raw))
		writeJSON(t, w, http.StatusOK, map[string]any{"id": 42, "version": 8, "title": "Refit"})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	_, _, err := client.PullRequestUpdate(t.Context(), "ENG", "warp-core", 42,
		PullRequestUpdateRequest{Version: 7, Title: "Refit"})
	require.NoError(t, err)

	// version and title are always sent; description, reviewers, and
	// toRef are omitted when the caller leaves them unset (preserve
	// semantics).
	assert.Contains(t, raw, "version")
	assert.Contains(t, raw, "title")
	assert.NotContains(t, raw, "description")
	assert.NotContains(t, raw, "reviewers")
	assert.NotContains(t, raw, "toRef")
}

func TestPullRequestUpdate_toRef(t *testing.T) {
	var gotBody PullRequestUpdateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		writeJSON(t, w, http.StatusOK, map[string]any{
			"id": 42, "version": 8, "title": "Refit",
			"toRef": map[string]any{"id": "refs/heads/develop", "displayId": "develop"},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	pr, _, err := client.PullRequestUpdate(t.Context(), "ENG", "warp-core", 42,
		PullRequestUpdateRequest{
			Version: 7,
			Title:   "Refit",
			ToRef:   &UpdateRef{ID: "refs/heads/develop"},
		})
	require.NoError(t, err)

	// The new base ref is carried as toRef.id in the PUT body.
	require.NotNil(t, gotBody.ToRef)
	assert.Equal(t, "refs/heads/develop", gotBody.ToRef.ID)
	assert.Equal(t, "develop", pr.ToRef.DisplayID)
}

func TestPullRequestUpdate_conflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusConflict, map[string]any{
			"errors": []map[string]any{
				{
					"message":       "The pull request has been updated since you last viewed it.",
					"exceptionName": "com.atlassian.bitbucket.pull.PullRequestOutOfDateException",
				},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	_, _, err := client.PullRequestUpdate(t.Context(), "ENG", "warp-core", 42,
		PullRequestUpdateRequest{Version: 1, Title: "stale"})
	require.ErrorIs(t, err, ErrConflict)
}

func TestPullRequestMerge(t *testing.T) {
	var (
		gotPath    string
		gotMethod  string
		gotVersion string
		gotBody    PullRequestMergeRequest
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotVersion = r.URL.Query().Get("version")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))

		writeJSON(t, w, http.StatusOK, map[string]any{
			"id":      42,
			"version": 9,
			"state":   "MERGED",
			"open":    false,
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	pr, resp, err := client.PullRequestMerge(t.Context(), "ENG", "warp-core", 42, 7,
		PullRequestMergeRequest{StrategyID: "squash"})
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/rest/api/1.0/projects/ENG/repos/warp-core/pull-requests/42/merge", gotPath)
	// The optimistic-locking version is carried in the query string.
	assert.Equal(t, "7", gotVersion)
	assert.Equal(t, "squash", gotBody.StrategyID)

	assert.Equal(t, "MERGED", pr.State)
}

func TestPullRequestMerge_defaultStrategy(t *testing.T) {
	var raw map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&raw))
		writeJSON(t, w, http.StatusOK, map[string]any{"id": 42, "version": 9, "state": "MERGED"})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	_, _, err := client.PullRequestMerge(t.Context(), "ENG", "warp-core", 42, 7,
		PullRequestMergeRequest{})
	require.NoError(t, err)

	// An empty StrategyID is omitted so the server applies its default.
	assert.NotContains(t, raw, "strategyId")
}

func TestPullRequestMerge_conflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusConflict, map[string]any{
			"errors": []map[string]any{
				{
					"message":       "stale version",
					"exceptionName": "com.atlassian.bitbucket.pull.PullRequestOutOfDateException",
				},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	_, _, err := client.PullRequestMerge(t.Context(), "ENG", "warp-core", 42, 1,
		PullRequestMergeRequest{})
	require.ErrorIs(t, err, ErrConflict)
}

func TestPullRequestCanMerge(t *testing.T) {
	var (
		gotPath   string
		gotMethod string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		writeJSON(t, w, http.StatusOK, map[string]any{
			"canMerge":   false,
			"conflicted": false,
			"outcome":    "UNKNOWN",
			"vetoes": []map[string]any{
				{
					"summaryMessage":  "requires 2 approvals",
					"detailedMessage": "This pull request requires 2 approvals before it can be merged.",
				},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	status, resp, err := client.PullRequestCanMerge(t.Context(), "ENG", "warp-core", 42)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, http.MethodGet, gotMethod)
	assert.Equal(t, "/rest/api/1.0/projects/ENG/repos/warp-core/pull-requests/42/merge", gotPath)

	assert.False(t, status.CanMerge)
	assert.False(t, status.Conflicted)
	assert.Equal(t, "UNKNOWN", status.Outcome)
	require.Len(t, status.Vetoes, 1)
	assert.Equal(t, "requires 2 approvals", status.Vetoes[0].SummaryMessage)
}

// TestClient_putDeleteRoundTrip exercises the put and delete HTTP verb
// helpers directly: a PUT round-trips a JSON body and decodes the
// response, and a DELETE carries a query string with no body.
func TestClient_putDeleteRoundTrip(t *testing.T) {
	var (
		putMethod   string
		putBody     map[string]any
		deleteQuery string
		deleteBody  []byte
		deleteCT    string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			putMethod = r.Method
			require.NoError(t, json.NewDecoder(r.Body).Decode(&putBody))
			writeJSON(t, w, http.StatusOK, map[string]any{"ok": true})
		case http.MethodDelete:
			deleteQuery = r.URL.RawQuery
			deleteCT = r.Header.Get("Content-Type")
			deleteBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected", http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	var dst map[string]any
	resp, err := client.put(t.Context(), "/thing", nil, map[string]any{"text": "hi"}, &dst)
	require.NoError(t, err)
	assert.Equal(t, http.MethodPut, putMethod)
	assert.Equal(t, "hi", putBody["text"])
	assert.Equal(t, true, dst["ok"])
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	resp, err = client.delete(t.Context(), "/thing", url.Values{"version": []string{"3"}})
	require.NoError(t, err)
	assert.Equal(t, "version=3", deleteQuery)
	assert.Empty(t, deleteBody, "delete must send no body")
	assert.Empty(t, deleteCT, "delete must not set a Content-Type with no body")
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestCommentCreate(t *testing.T) {
	var (
		gotPath   string
		gotMethod string
		gotBody   map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))

		writeJSON(t, w, http.StatusCreated, map[string]any{
			"id":      101,
			"version": 0,
			"text":    gotBody["text"],
			"author":  map[string]any{"name": "jcaptain"},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	comment, resp, err := client.CommentCreate(t.Context(), "ENG", "warp-core", 42, "looks good")
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t,
		"/rest/api/1.0/projects/ENG/repos/warp-core/pull-requests/42/comments",
		gotPath)
	assert.Equal(t, "looks good", gotBody["text"])
	// Create must not carry a version in the body.
	assert.NotContains(t, gotBody, "version")

	// The created comment's id and version are decoded so callers can
	// later update or delete it.
	assert.Equal(t, int64(101), comment.ID)
	assert.Equal(t, 0, comment.Version)
	assert.Equal(t, "looks good", comment.Text)
	assert.Equal(t, "jcaptain", comment.Author.Name)
}

func TestCommentUpdate(t *testing.T) {
	var (
		gotPath   string
		gotMethod string
		gotBody   map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))

		writeJSON(t, w, http.StatusOK, map[string]any{
			"id":      101,
			"version": 2,
			"text":    gotBody["text"],
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	comment, resp, err := client.CommentUpdate(t.Context(), "ENG", "warp-core", 42, 101, "edited", 1)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, http.MethodPut, gotMethod)
	assert.Equal(t,
		"/rest/api/1.0/projects/ENG/repos/warp-core/pull-requests/42/comments/101",
		gotPath)
	// The PUT body carries both the new text and the optimistic-locking
	// version.
	assert.Equal(t, "edited", gotBody["text"])
	assert.EqualValues(t, 1, gotBody["version"])

	assert.Equal(t, int64(101), comment.ID)
	assert.Equal(t, 2, comment.Version)
	assert.Equal(t, "edited", comment.Text)
}

func TestCommentUpdate_conflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusConflict, map[string]any{
			"errors": []map[string]any{
				{
					"message":       "The comment has been modified since you last viewed it.",
					"exceptionName": "com.atlassian.bitbucket.comment.CommentOutOfDateException",
				},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	_, _, err := client.CommentUpdate(t.Context(), "ENG", "warp-core", 42, 101, "stale", 1)
	require.ErrorIs(t, err, ErrConflict)
}

func TestCommentDelete(t *testing.T) {
	var (
		gotPath    string
		gotMethod  string
		gotVersion string
		gotBody    []byte
		gotCT      string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotVersion = r.URL.Query().Get("version")
		gotCT = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	resp, err := client.CommentDelete(t.Context(), "ENG", "warp-core", 42, 101, 3)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, http.MethodDelete, gotMethod)
	assert.Equal(t,
		"/rest/api/1.0/projects/ENG/repos/warp-core/pull-requests/42/comments/101",
		gotPath)
	// The optimistic-locking version is carried in the query string with
	// no request body.
	assert.Equal(t, "3", gotVersion)
	assert.Empty(t, gotBody, "delete must send no body")
	assert.Empty(t, gotCT, "delete must not set a Content-Type with no body")
}

func TestCommentDelete_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	_, err := client.CommentDelete(t.Context(), "ENG", "warp-core", 42, 999, 1)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestActivityList(t *testing.T) {
	var requests []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Query())
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t,
			"/rest/api/1.0/projects/ENG/repos/warp-core/pull-requests/42/activities",
			r.URL.Path)

		switch r.URL.Query().Get("start") {
		case "0":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"values": []map[string]any{
					{"id": 1, "action": "OPENED"},
					{
						"id":     2,
						"action": "COMMENTED",
						"comment": map[string]any{
							"id":             101,
							"version":        0,
							"text":           "first comment",
							"author":         map[string]any{"name": "jcaptain"},
							"severity":       "NORMAL",
							"state":          "RESOLVED",
							"threadResolved": true,
						},
					},
				},
				"isLastPage":    false,
				"nextPageStart": 2,
			})
		case "2":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"values": []map[string]any{
					{
						"id":     3,
						"action": "COMMENTED",
						"comment": map[string]any{
							"id":       102,
							"version":  4,
							"text":     "second comment",
							"severity": "BLOCKER",
							"state":    "OPEN",
						},
					},
					{"id": 4, "action": "MERGED"},
				},
				"isLastPage": true,
			})
		default:
			t.Errorf("unexpected start: %q", r.URL.Query().Get("start"))
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	var activities []Activity
	for a, err := range client.ActivityList(t.Context(), "ENG", "warp-core", 42) {
		require.NoError(t, err)
		activities = append(activities, a)
	}

	require.Len(t, activities, 4)
	require.Len(t, requests, 2)

	// Non-comment activities decode with a nil Comment.
	assert.Equal(t, "OPENED", activities[0].Action)
	assert.Nil(t, activities[0].Comment)
	assert.Equal(t, "MERGED", activities[3].Action)
	assert.Nil(t, activities[3].Comment)

	// COMMENTED activities carry a non-nil comment with text and version.
	require.Equal(t, ActivityActionCommented, activities[1].Action)
	require.NotNil(t, activities[1].Comment)
	assert.Equal(t, int64(101), activities[1].Comment.ID)
	assert.Equal(t, 0, activities[1].Comment.Version)
	assert.Equal(t, "first comment", activities[1].Comment.Text)
	assert.Equal(t, "jcaptain", activities[1].Comment.Author.Name)
	// Resolution fields decode off the same feed.
	assert.Equal(t, "NORMAL", activities[1].Comment.Severity)
	assert.Equal(t, "RESOLVED", activities[1].Comment.State)
	assert.True(t, activities[1].Comment.ThreadResolved)

	require.Equal(t, ActivityActionCommented, activities[2].Action)
	require.NotNil(t, activities[2].Comment)
	assert.Equal(t, int64(102), activities[2].Comment.ID)
	assert.Equal(t, 4, activities[2].Comment.Version)
	assert.Equal(t, "second comment", activities[2].Comment.Text)
	// An open task: blocker severity, unresolved.
	assert.Equal(t, "BLOCKER", activities[2].Comment.Severity)
	assert.Equal(t, "OPEN", activities[2].Comment.State)
	assert.False(t, activities[2].Comment.ThreadResolved)
}

func TestApplicationProperties(t *testing.T) {
	var (
		gotPath   string
		gotMethod string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		writeJSON(t, w, http.StatusOK, map[string]any{
			"version":     "9.4.0",
			"buildNumber": "9004000",
			"buildDate":   "1700000000000",
			"displayName": "Bitbucket",
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	props, err := client.ApplicationProperties(t.Context())
	require.NoError(t, err)

	assert.Equal(t, http.MethodGet, gotMethod)
	assert.Equal(t, "/rest/api/1.0/application-properties", gotPath)

	assert.Equal(t, "9.4.0", props.Version)
	assert.Equal(t, "9004000", props.BuildNumber)
}

func TestApplicationProperties_memoized(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		writeJSON(t, w, http.StatusOK, map[string]any{"version": "9.4.0"})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	first, err := client.ApplicationProperties(t.Context())
	require.NoError(t, err)
	second, err := client.ApplicationProperties(t.Context())
	require.NoError(t, err)

	// The first success is cached, so the second call makes no request and
	// returns the same value.
	assert.Equal(t, 1, requests, "only the first call should hit the server")
	assert.Equal(t, "9.4.0", first.Version)
	assert.Same(t, first, second)
}

func TestApplicationProperties_successOnlyRetry(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests == 1 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"version": "9.4.0"})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	// A failed first call is not cached, so the second call retries and
	// succeeds.
	_, err := client.ApplicationProperties(t.Context())
	require.Error(t, err)

	props, err := client.ApplicationProperties(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "9.4.0", props.Version)
	assert.Equal(t, 2, requests)
}

func TestRepositoryGet(t *testing.T) {
	var (
		gotPath   string
		gotMethod string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		writeJSON(t, w, http.StatusOK, map[string]any{
			"id":   42,
			"slug": "warp-core",
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	repo, resp, err := client.RepositoryGet(t.Context(), "ENG", "warp-core")
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, http.MethodGet, gotMethod)
	assert.Equal(t, "/rest/api/1.0/projects/ENG/repos/warp-core", gotPath)

	assert.Equal(t, int64(42), repo.ID)
}

func TestRepositoryGet_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	_, _, err := client.RepositoryGet(t.Context(), "ENG", "missing")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestDefaultReviewers(t *testing.T) {
	var (
		gotPath   string
		gotMethod string
		gotQuery  url.Values
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotQuery = r.URL.Query()
		// The endpoint responds with a bare JSON array, not a paginated
		// envelope.
		writeJSON(t, w, http.StatusOK, []map[string]any{
			{"name": "alice", "id": 10},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	reviewers, resp, err := client.DefaultReviewers(
		t.Context(), "ENG", "warp-core", 42, 42,
		"refs/heads/feature", "refs/heads/main",
	)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, http.MethodGet, gotMethod)
	// The call is routed to the default-reviewers REST API root.
	assert.Equal(t,
		"/rest/default-reviewers/1.0/projects/ENG/repos/warp-core/reviewers",
		gotPath)

	// The source/target repo IDs and fully qualified refs are carried as
	// query parameters.
	assert.Equal(t, "42", gotQuery.Get("sourceRepoId"))
	assert.Equal(t, "42", gotQuery.Get("targetRepoId"))
	assert.Equal(t, "refs/heads/feature", gotQuery.Get("sourceRefId"))
	assert.Equal(t, "refs/heads/main", gotQuery.Get("targetRefId"))

	// The bare array decodes into a slice of default reviewers.
	require.Len(t, reviewers, 1)
	assert.Equal(t, "alice", reviewers[0].Name)
}

func TestBlockerCommentList(t *testing.T) {
	var requests []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Query())
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t,
			"/rest/api/1.0/projects/ENG/repos/warp-core/pull-requests/42/blocker-comments",
			r.URL.Path)

		switch r.URL.Query().Get("start") {
		case "0":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"values": []map[string]any{
					{
						"id":       201,
						"version":  0,
						"text":     "fix the docs",
						"author":   map[string]any{"name": "jcaptain"},
						"severity": "BLOCKER",
						"state":    "OPEN",
					},
				},
				"isLastPage":    false,
				"nextPageStart": 1,
			})
		case "1":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"values": []map[string]any{
					{
						"id":       202,
						"version":  2,
						"text":     "address review",
						"severity": "BLOCKER",
						"state":    "RESOLVED",
					},
				},
				"isLastPage": true,
			})
		default:
			t.Errorf("unexpected start: %q", r.URL.Query().Get("start"))
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	var tasks []Comment
	for c, err := range client.BlockerCommentList(t.Context(), "ENG", "warp-core", 42) {
		require.NoError(t, err)
		tasks = append(tasks, c)
	}

	// The unfiltered endpoint returns tasks in both states across pages.
	require.Len(t, tasks, 2)
	require.Len(t, requests, 2)

	assert.Equal(t, int64(201), tasks[0].ID)
	assert.Equal(t, "fix the docs", tasks[0].Text)
	assert.Equal(t, "jcaptain", tasks[0].Author.Name)
	assert.Equal(t, "BLOCKER", tasks[0].Severity)
	assert.Equal(t, "OPEN", tasks[0].State)

	assert.Equal(t, int64(202), tasks[1].ID)
	assert.Equal(t, 2, tasks[1].Version)
	assert.Equal(t, "RESOLVED", tasks[1].State)
}

func TestRawFileGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t,
			"/rest/api/1.0/projects/ENG/repos/warp-core/raw/.bitbucket/PULL_REQUEST_TEMPLATE.md",
			r.URL.Path)
		// Omitting "at" makes the server use the default branch.
		assert.Empty(t, r.URL.Query().Get("at"))

		w.Header().Set("Content-Type", "text/plain")
		_, err := io.WriteString(w, "## Summary\n")
		assert.NoError(t, err)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	body, resp, err := client.RawFileGet(
		t.Context(), "ENG", "warp-core",
		".bitbucket/PULL_REQUEST_TEMPLATE.md",
	)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "## Summary\n", string(body))
}

func TestRawFileGet_personalProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t,
			"/rest/api/1.0/projects/~jcaptain/repos/warp-core/raw/PULL_REQUEST_TEMPLATE.md",
			r.URL.Path)

		_, err := io.WriteString(w, "body")
		assert.NoError(t, err)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	body, _, err := client.RawFileGet(
		t.Context(), "~jcaptain", "warp-core",
		"PULL_REQUEST_TEMPLATE.md",
	)
	require.NoError(t, err)
	assert.Equal(t, "body", string(body))
}

func TestRawFileGet_escapesPathSegments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t,
			"/rest/api/1.0/projects/ENG/repos/warp-core/raw/docs%20dir/template.md",
			r.URL.EscapedPath())

		_, err := io.WriteString(w, "body")
		assert.NoError(t, err)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	body, _, err := client.RawFileGet(
		t.Context(), "ENG", "warp-core",
		"docs dir/template.md",
	)
	require.NoError(t, err)
	assert.Equal(t, "body", string(body))
}

func TestRawFileGet_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	_, _, err := client.RawFileGet(
		t.Context(), "ENG", "warp-core",
		"PULL_REQUEST_TEMPLATE.md",
	)
	require.ErrorIs(t, err, ErrNotFound)
}
