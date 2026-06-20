package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
)

// _navigationCommentMarker mirrors the invisible marker that the forge
// plants in git-spice's navigation comments;
// the gateway must pass such comments through unfiltered.
const _navigationCommentMarker = "[gs]: # (navigation comment)"

func TestGateway_CreateComment(t *testing.T) {
	var gotBody struct {
		Text string `json:"text"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, commentsPath(7), r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		gatewayWriteJSON(t, w, http.StatusCreated, map[string]any{
			"id":      101,
			"version": 0,
			"text":    gotBody.Text,
		})
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	comment, err := gw.CreateComment(t.Context(), 7, "hello world")
	require.NoError(t, err)

	assert.Equal(t, "hello world", gotBody.Text)
	assert.Equal(t, int64(101), comment.ID)
	assert.Equal(t, int64(7), comment.PRID)
	assert.Equal(t, 0, comment.Version)
	assert.Equal(t, "hello world", comment.Body)
}

func TestGateway_UpdateComment(t *testing.T) {
	var (
		gotVersion int
		gotText    string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPut, r.Method)
		require.Equal(t, commentItemPath(7, 101), r.URL.Path)
		var body struct {
			Text    string `json:"text"`
			Version *int   `json:"version"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		gotText = body.Text
		require.NotNil(t, body.Version)
		gotVersion = *body.Version
		gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
			"id": 101, "version": 3, "text": body.Text,
		})
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	err := gw.UpdateComment(t.Context(),
		&bitbucket.ChangeComment{ID: 101, PRID: 7, Version: 2}, "updated body")
	require.NoError(t, err)

	assert.Equal(t, "updated body", gotText)
	assert.Equal(t, 2, gotVersion)
}

func TestGateway_UpdateComment_conflictRefetchRetry(t *testing.T) {
	var (
		putCount int
		versions []int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == commentItemPath(7, 101):
			putCount++
			var body struct {
				Version *int `json:"version"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.NotNil(t, body.Version)
			versions = append(versions, *body.Version)
			if putCount == 1 {
				// Reject the first update with a version conflict.
				gatewayWriteJSON(t, w, http.StatusConflict, map[string]any{
					"errors": []map[string]any{{"message": "comment out of date"}},
				})
				return
			}
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"id": 101, "version": 10})
		case r.Method == http.MethodGet && r.URL.Path == activitiesPath(7):
			// The live version is 9, newer than the stale persisted 2.
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
				"isLastPage": true,
				"values": []map[string]any{
					{"action": "OPENED"},
					{
						"action": "COMMENTED",
						"comment": map[string]any{
							"id": 101, "version": 9, "text": "x",
						},
					},
				},
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	err := gw.UpdateComment(t.Context(),
		&bitbucket.ChangeComment{ID: 101, PRID: 7, Version: 2}, "updated body")
	require.NoError(t, err)

	assert.Equal(t, 2, putCount, "expected the update to be retried once")
	// First attempt uses the stale version; the retry uses the refreshed one.
	assert.Equal(t, []int{2, 9}, versions)
}

func TestGateway_UpdateComment_deletedRecreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			return
		}
		// Comment was deleted between read and update.
		http.NotFound(w, r)
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	err := gw.UpdateComment(t.Context(),
		&bitbucket.ChangeComment{ID: 101, PRID: 7, Version: 2}, "updated body")
	require.Error(t, err)
	// The sentinel that tells the caller to recreate the comment.
	assert.ErrorIs(t, err, forge.ErrNotFound)
}

func TestGateway_UpdateComment_conflictThenGone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut:
			gatewayWriteJSON(t, w, http.StatusConflict, map[string]any{
				"errors": []map[string]any{{"message": "out of date"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == activitiesPath(7):
			// The comment is absent from the feed: it was deleted.
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
				"isLastPage": true,
				"values": []map[string]any{
					{"action": "OPENED"},
				},
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	err := gw.UpdateComment(t.Context(),
		&bitbucket.ChangeComment{ID: 101, PRID: 7, Version: 2}, "updated body")
	require.Error(t, err)
	assert.ErrorIs(t, err, forge.ErrNotFound)
}

func TestGateway_DeleteComment(t *testing.T) {
	var gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodDelete, r.Method)
		require.Equal(t, commentItemPath(7, 101), r.URL.Path)
		gotVersion = r.URL.Query().Get("version")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	err := gw.DeleteComment(t.Context(),
		&bitbucket.ChangeComment{ID: 101, PRID: 7, Version: 4})
	require.NoError(t, err)

	// The optimistic-locking version travels in the query string.
	assert.Equal(t, "4", gotVersion)
}

func TestGateway_DeleteComment_conflictRefetchRetry(t *testing.T) {
	var (
		deleteCount int
		versions    []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == commentItemPath(7, 101):
			deleteCount++
			versions = append(versions, r.URL.Query().Get("version"))
			if deleteCount == 1 {
				gatewayWriteJSON(t, w, http.StatusConflict, map[string]any{
					"errors": []map[string]any{{"message": "out of date"}},
				})
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == activitiesPath(7):
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
				"isLastPage": true,
				"values": []map[string]any{
					{
						"action": "COMMENTED",
						"comment": map[string]any{
							"id": 101, "version": 12,
						},
					},
				},
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	err := gw.DeleteComment(t.Context(),
		&bitbucket.ChangeComment{ID: 101, PRID: 7, Version: 4})
	require.NoError(t, err)

	assert.Equal(t, 2, deleteCount, "expected the delete to be retried once")
	assert.Equal(t, []string{"4", "12"}, versions)
}

func TestGateway_DeleteComment_conflictThenGone(t *testing.T) {
	var deleteCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == commentItemPath(7, 101):
			deleteCount++
			gatewayWriteJSON(t, w, http.StatusConflict, map[string]any{
				"errors": []map[string]any{{"message": "out of date"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == activitiesPath(7):
			// The comment is absent from the feed: it was already deleted.
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
				"isLastPage": true,
				"values": []map[string]any{
					{"action": "OPENED"},
				},
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	// A comment that disappeared after the conflict deletes cleanly:
	// there is nothing left to delete.
	gw := newOpsTestServerGateway(t, srv)
	err := gw.DeleteComment(t.Context(),
		&bitbucket.ChangeComment{ID: 101, PRID: 7, Version: 4})
	require.NoError(t, err)
	assert.Equal(t, 1, deleteCount, "the delete must not be retried")
}

func TestGateway_DeleteComment_alreadyDeleted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodDelete, r.Method)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// A comment that is already gone deletes cleanly.
	gw := newOpsTestServerGateway(t, srv)
	err := gw.DeleteComment(t.Context(),
		&bitbucket.ChangeComment{ID: 101, PRID: 7, Version: 4})
	require.NoError(t, err)
}

func TestGateway_commentRoundTrip(t *testing.T) {
	var (
		created  bool
		updated  bool
		deleted  bool
		lastText string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == commentsPath(7):
			created = true
			var body struct {
				Text string `json:"text"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			lastText = body.Text
			gatewayWriteJSON(t, w, http.StatusCreated, map[string]any{
				"id": 55, "version": 0, "text": body.Text,
			})
		case r.Method == http.MethodPut && r.URL.Path == commentItemPath(7, 55):
			updated = true
			var body struct {
				Text string `json:"text"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			lastText = body.Text
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
				"id": 55, "version": 1, "text": body.Text,
			})
		case r.Method == http.MethodDelete && r.URL.Path == commentItemPath(7, 55):
			deleted = true
			assert.Equal(t, "1", r.URL.Query().Get("version"))
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	ctx := t.Context()

	comment, err := gw.CreateComment(ctx, 7, "v1")
	require.NoError(t, err)
	require.True(t, created)
	assert.Equal(t, "v1", lastText)

	require.NoError(t, gw.UpdateComment(ctx, comment, "v2"))
	require.True(t, updated)
	assert.Equal(t, "v2", lastText)

	// Track the version returned by the update so delete sends the right one.
	updatedComment := &bitbucket.ChangeComment{ID: comment.ID, PRID: comment.PRID, Version: 1}
	require.NoError(t, gw.DeleteComment(ctx, updatedComment))
	require.True(t, deleted)
}

func TestGateway_liveCommentVersion(t *testing.T) {
	t.Run("Found", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, activitiesPath(7), r.URL.Path)
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
				"isLastPage": true,
				"values": []map[string]any{
					// Non-comment activity must be skipped.
					{"action": "OPENED"},
					// Different comment ID must be skipped.
					{"action": "COMMENTED", "comment": map[string]any{"id": 100, "version": 1}},
					// Target comment: its live version is returned.
					{"action": "COMMENTED", "comment": map[string]any{"id": 101, "version": 5}},
				},
			})
		}))
		defer srv.Close()

		gw := newOpsTestServerGateway(t, srv)
		version, found, err := gw.liveCommentVersion(t.Context(), 7, 101)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, 5, version)
	})

	t.Run("NotFound", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
				"isLastPage": true,
				"values": []map[string]any{
					{"action": "COMMENTED", "comment": map[string]any{"id": 100, "version": 1}},
				},
			})
		}))
		defer srv.Close()

		gw := newOpsTestServerGateway(t, srv)
		version, found, err := gw.liveCommentVersion(t.Context(), 7, 101)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Zero(t, version)
	})

	t.Run("Error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer srv.Close()

		gw := newOpsTestServerGateway(t, srv)
		_, found, err := gw.liveCommentVersion(t.Context(), 7, 101)
		require.Error(t, err)
		assert.False(t, found)
	})
}

func TestGateway_ListComments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, activitiesPath(7), r.URL.Path)
		gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
			"isLastPage": true,
			"values": []map[string]any{
				{"action": "OPENED"},
				{
					"action": "COMMENTED",
					"comment": map[string]any{
						"id": 1, "version": 0, "text": "first comment",
						"author": map[string]any{"name": "alice"},
					},
				},
				{"action": "RESCOPED"},
				{
					"action": "COMMENTED",
					"comment": map[string]any{
						"id": 2, "version": 1, "text": "second comment",
						"author": map[string]any{"name": "bob"},
					},
				},
			},
		})
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)

	var got []*bitbucket.ChangeComment
	for comment, err := range gw.ListComments(t.Context(), 7, bitbucket.ListCommentsOptions{}) {
		require.NoError(t, err)
		got = append(got, comment)
	}

	// Only COMMENTED activities are surfaced, in feed order.
	assert.Equal(t, []*bitbucket.ChangeComment{
		{ID: 1, PRID: 7, Version: 0, Body: "first comment"},
		{ID: 2, PRID: 7, Version: 1, Body: "second comment"},
	}, got)
}

func TestGateway_ListComments_canUpdateOnlyFiltersToSelf(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/1.0/application-properties":
			// CurrentUser identifies the authenticated user via X-AUSERNAME.
			w.Header().Set("X-AUSERNAME", "alice")
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"version": "9.4.0"})
		case r.URL.Path == activitiesPath(7):
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
				"isLastPage": true,
				"values": []map[string]any{
					{
						"action": "COMMENTED",
						"comment": map[string]any{
							"id": 1, "text": "mine",
							"author": map[string]any{"name": "alice"},
						},
					},
					{
						"action": "COMMENTED",
						"comment": map[string]any{
							"id": 2, "text": "theirs",
							"author": map[string]any{"name": "bob"},
						},
					},
				},
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)

	var got []*bitbucket.ChangeComment
	for comment, err := range gw.ListComments(t.Context(), 7,
		bitbucket.ListCommentsOptions{CanUpdateOnly: true}) {
		require.NoError(t, err)
		got = append(got, comment)
	}

	// Only the comment authored by the authenticated user (alice) remains.
	require.Len(t, got, 1)
	assert.Equal(t, int64(1), got[0].ID)
	assert.Equal(t, "mine", got[0].Body)
}

func TestGateway_ListComments_currentUserError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/1.0/application-properties" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		// The current-user lookup fails,
		// so the CanUpdateOnly filter cannot be applied.
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)

	var gotErr error
	for _, err := range gw.ListComments(t.Context(), 7,
		bitbucket.ListCommentsOptions{CanUpdateOnly: true}) {
		gotErr = err
		break
	}
	require.Error(t, gotErr)
	assert.ErrorContains(t, gotErr, "get current user")
}

func TestGateway_ResolvableComments(t *testing.T) {
	// The activity feed mixes resolved/unresolved comment threads (general
	// and inline), completed/open tasks, an unpublished draft, and
	// git-spice's own navigation comment. Unlike the old per-product
	// repository, the gateway emits drafts (with Pending set) and the
	// navigation comment; the caller is responsible for filtering them.
	feed := []map[string]any{
		{"action": "OPENED"},
		{"action": "COMMENTED", "comment": map[string]any{
			"id": 10, "text": "resolved general comment",
			"severity": "NORMAL", "state": "OPEN", "threadResolved": true,
		}},
		{"action": "COMMENTED", "comment": map[string]any{
			"id": 11, "text": "open general comment",
			"severity": "NORMAL", "state": "OPEN", "threadResolved": false,
		}},
		{"action": "COMMENTED", "comment": map[string]any{
			"id": 12, "text": "open inline comment",
			"severity": "NORMAL", "state": "OPEN", "threadResolved": false,
			"anchor": map[string]any{"path": "src/Main.java", "line": 10},
		}},
		{"action": "COMMENTED", "comment": map[string]any{
			"id": 13, "text": "completed task",
			"severity": "BLOCKER", "state": "RESOLVED", "threadResolved": false,
		}},
		{"action": "COMMENTED", "comment": map[string]any{
			"id": 14, "text": "open task",
			"severity": "BLOCKER", "state": "OPEN", "threadResolved": false,
		}},
		// An unpublished draft, visible only to its author.
		{"action": "COMMENTED", "comment": map[string]any{
			"id": 15, "text": "pending draft", "state": "PENDING",
		}},
		// git-spice's own navigation comment.
		{"action": "COMMENTED", "comment": map[string]any{
			"id": 16, "text": "stack\n\n" + _navigationCommentMarker,
			"severity": "NORMAL", "state": "OPEN",
		}},
	}

	// The flat task list re-reports the two top-level tasks (13, 14); the
	// dedup by ID must keep them from being emitted a second time.
	tasks := []map[string]any{
		{"id": 13, "text": "completed task", "severity": "BLOCKER", "state": "RESOLVED"},
		{"id": 14, "text": "open task", "severity": "BLOCKER", "state": "OPEN"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		switch r.URL.Path {
		case activitiesPath(1):
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"isLastPage": true, "values": feed})
		case blockerCommentsPath(1):
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"isLastPage": true, "values": tasks})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)

	var got []*bitbucket.ResolvableComment
	for comment, err := range gw.ResolvableComments(t.Context(), 1) {
		require.NoError(t, err)
		got = append(got, comment)
	}

	assert.Equal(t, []*bitbucket.ResolvableComment{
		{ID: 10, Body: "resolved general comment", Resolved: true},
		{ID: 11, Body: "open general comment"},
		{ID: 12, Body: "open inline comment"},
		{ID: 13, Body: "completed task", Resolved: true},
		{ID: 14, Body: "open task"},
		{ID: 15, Body: "pending draft", Pending: true},
		{ID: 16, Body: "stack\n\n" + _navigationCommentMarker},
	}, got)
}

func TestGateway_ResolvableComments_tasks(t *testing.T) {
	// Each case exercises one facet of folding the flat task list into the
	// activity-feed comments. The feed surfaces only thread roots, so a
	// task nested as a reply arrives solely via the task list.
	tests := []struct {
		name string

		feed []map[string]any
		// tasks is the blocker-comments response,
		// or nil to make the endpoint 404 like a pre-7.2 server.
		tasks []map[string]any

		want []*bitbucket.ResolvableComment
	}{
		{
			// A task nested as a reply: absent from the feed, added here.
			name: "NestedTaskAdded",
			feed: []map[string]any{{"action": "COMMENTED", "comment": map[string]any{
				"id": 20, "text": "please fix", "severity": "NORMAL", "state": "OPEN",
			}}},
			tasks: []map[string]any{
				{"id": 99, "text": "nested task", "severity": "BLOCKER", "state": "OPEN"},
			},
			want: []*bitbucket.ResolvableComment{
				{ID: 20, Body: "please fix"},
				{ID: 99, Body: "nested task"},
			},
		},
		{
			// A resolved nested task.
			name: "NestedTaskResolved",
			feed: []map[string]any{{"action": "COMMENTED", "comment": map[string]any{
				"id": 30, "text": "nit", "severity": "NORMAL", "state": "OPEN",
			}}},
			tasks: []map[string]any{
				{"id": 88, "text": "done nested task", "severity": "BLOCKER", "state": "RESOLVED"},
			},
			want: []*bitbucket.ResolvableComment{
				{ID: 30, Body: "nit"},
				{ID: 88, Body: "done nested task", Resolved: true},
			},
		},
		{
			// The top-level task (deduped against the feed)
			// plus a nested one (added).
			name: "TopLevelTaskDeduped",
			feed: []map[string]any{{"action": "COMMENTED", "comment": map[string]any{
				"id": 40, "text": "top-level task", "severity": "BLOCKER", "state": "OPEN",
			}}},
			tasks: []map[string]any{
				{"id": 40, "text": "top-level task", "severity": "BLOCKER", "state": "OPEN"},
				{"id": 41, "text": "nested task", "severity": "BLOCKER", "state": "OPEN"},
			},
			want: []*bitbucket.ResolvableComment{
				{ID: 40, Body: "top-level task"},
				{ID: 41, Body: "nested task"},
			},
		},
		{
			// The endpoint 404s like a pre-7.2 server;
			// the activity-feed result stands, no error.
			name: "Pre72NoEndpoint",
			feed: []map[string]any{{"action": "COMMENTED", "comment": map[string]any{
				"id": 50, "text": "general comment", "severity": "NORMAL", "state": "OPEN",
			}}},
			tasks: nil,
			want: []*bitbucket.ResolvableComment{
				{ID: 50, Body: "general comment"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				switch r.URL.Path {
				case activitiesPath(1):
					gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"isLastPage": true, "values": tt.feed})
				case blockerCommentsPath(1):
					if tt.tasks == nil {
						http.Error(w, "not found", http.StatusNotFound)
						return
					}
					gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"isLastPage": true, "values": tt.tasks})
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
			}))
			defer srv.Close()

			gw := newOpsTestServerGateway(t, srv)

			var got []*bitbucket.ResolvableComment
			for comment, err := range gw.ResolvableComments(t.Context(), 1) {
				require.NoError(t, err)
				got = append(got, comment)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGateway_ResolvableComments_blockerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case activitiesPath(1):
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
				"isLastPage": true,
				"values": []map[string]any{
					{"action": "COMMENTED", "comment": map[string]any{
						"id": 10, "text": "a comment", "severity": "NORMAL", "state": "OPEN",
					}},
				},
			})
		case blockerCommentsPath(1):
			// Only a 404 is tolerated; any other failure must surface.
			http.Error(w, "boom", http.StatusInternalServerError)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)

	var (
		got    []*bitbucket.ResolvableComment
		gotErr error
	)
	for comment, err := range gw.ResolvableComments(t.Context(), 1) {
		if err != nil {
			gotErr = err
			break
		}
		got = append(got, comment)
	}

	// The feed comment is still emitted before the task list fails.
	require.Len(t, got, 1)
	assert.Equal(t, int64(10), got[0].ID)
	require.Error(t, gotErr)
	assert.ErrorContains(t, gotErr, "list blocker comments")
}

// commentsPath is the REST path for creating/listing comments on a PR.
func commentsPath(prID int64) string {
	return prItemPath(prID) + "/comments"
}

// commentItemPath is the REST path for a single comment on a PR.
func commentItemPath(prID, commentID int64) string {
	return commentsPath(prID) + "/" + strconv.FormatInt(commentID, 10)
}

// activitiesPath is the REST path for a PR's activity feed.
func activitiesPath(prID int64) string {
	return prItemPath(prID) + "/activities"
}

// blockerCommentsPath is the REST path for a PR's flat task list.
func blockerCommentsPath(prID int64) string {
	return prItemPath(prID) + "/blocker-comments"
}
