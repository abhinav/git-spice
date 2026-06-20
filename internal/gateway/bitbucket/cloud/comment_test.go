package cloud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
	"go.abhg.dev/testing/stub"
)

func TestGateway_CreateComment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t,
			"/repositories/workspace/repo/pullrequests/1/comments",
			r.URL.Path)

		var req CommentCreateRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "test comment", req.Content.Raw)

		assert.NoError(t, json.NewEncoder(w).Encode(Comment{
			ID:      42,
			Content: Content{Raw: req.Content.Raw},
		}))
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	comment, err := gw.CreateComment(t.Context(), 1, "test comment")
	require.NoError(t, err)

	// Version must stay zero: Bitbucket Cloud comments are not versioned.
	assert.Equal(t, &bitbucket.ChangeComment{
		ID:   42,
		PRID: 1,
		Body: "test comment",
	}, comment)
}

func TestGateway_CreateComment_error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	_, err := gw.CreateComment(t.Context(), 1, "test comment")
	require.Error(t, err)
	assert.ErrorContains(t, err, "create comment")
}

func TestGateway_UpdateComment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t,
			"/repositories/workspace/repo/pullrequests/123/comments/42",
			r.URL.Path)

		var req CommentCreateRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "updated content", req.Content.Raw)

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	err := gw.UpdateComment(
		t.Context(), &bitbucket.ChangeComment{ID: 42, PRID: 123}, "updated content",
	)
	require.NoError(t, err)
}

func TestGateway_UpdateComment_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"type":"error","error":{"message":"Comment not found"}}`))
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	err := gw.UpdateComment(
		t.Context(), &bitbucket.ChangeComment{ID: 42, PRID: 123}, "updated content",
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, forge.ErrNotFound)
}

func TestGateway_DeleteComment(t *testing.T) {
	var deleted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t,
			"/repositories/workspace/repo/pullrequests/123/comments/42",
			r.URL.Path)
		deleted = true

		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	err := gw.DeleteComment(t.Context(), &bitbucket.ChangeComment{ID: 42, PRID: 123})
	require.NoError(t, err)
	assert.True(t, deleted)
}

func TestGateway_DeleteComment_error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	err := gw.DeleteComment(t.Context(), &bitbucket.ChangeComment{ID: 42, PRID: 123})
	require.Error(t, err)
	assert.ErrorContains(t, err, "delete comment")
}

func TestGateway_ListComments(t *testing.T) {
	tests := []struct {
		name     string
		comments []Comment
		opts     bitbucket.ListCommentsOptions
		want     []*bitbucket.ChangeComment
	}{
		{
			name: "Comments",
			comments: []Comment{
				{ID: 1, Content: Content{Raw: "hello"}},
				{ID: 2, Content: Content{Raw: "world"}},
			},
			want: []*bitbucket.ChangeComment{
				{ID: 1, PRID: 1, Body: "hello"},
				{ID: 2, PRID: 1, Body: "world"},
			},
		},
		{
			name:     "Empty",
			comments: []Comment{},
		},
		{
			// Bitbucket Cloud cannot filter comments by author,
			// so CanUpdateOnly is a best-effort no-op.
			name: "CanUpdateOnlyIgnored",
			comments: []Comment{
				{ID: 1, Content: Content{Raw: "hello"}},
			},
			opts: bitbucket.ListCommentsOptions{CanUpdateOnly: true},
			want: []*bitbucket.ChangeComment{
				{ID: 1, PRID: 1, Body: "hello"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t,
					"/repositories/workspace/repo/pullrequests/1/comments",
					r.URL.Path)
				assert.NoError(t, json.NewEncoder(w).Encode(
					CommentList{Values: tt.comments}))
			}))
			defer srv.Close()

			gw := newTestGateway(t, srv.URL)

			var got []*bitbucket.ChangeComment
			for comment, err := range gw.ListComments(t.Context(), 1, tt.opts) {
				require.NoError(t, err)
				got = append(got, comment)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGateway_ListComments_absoluteNextURL(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.RawQuery {
		case "pagelen=100":
			assert.NoError(t, json.NewEncoder(w).Encode(CommentList{
				Values: []Comment{
					{ID: 1, Content: Content{Raw: "first"}},
				},
				Next: srv.URL +
					"/repositories/workspace/repo/pullrequests/1/comments?page=2",
			}))
		default:
			assert.NoError(t, json.NewEncoder(w).Encode(CommentList{
				Values: []Comment{
					{ID: 2, Content: Content{Raw: "second"}},
				},
			}))
		}
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)

	var got []*bitbucket.ChangeComment
	for comment, err := range gw.ListComments(t.Context(), 1, bitbucket.ListCommentsOptions{}) {
		require.NoError(t, err)
		got = append(got, comment)
	}

	assert.Equal(t, []*bitbucket.ChangeComment{
		{ID: 1, PRID: 1, Body: "first"},
		{ID: 2, PRID: 1, Body: "second"},
	}, got)
}

func TestGateway_ListComments_pageSize(t *testing.T) {
	SetListChangeCommentsPageSize(t, 1)

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.RawQuery {
		case "pagelen=1":
			assert.NoError(t, json.NewEncoder(w).Encode(CommentList{
				Values: []Comment{
					{ID: 1, Content: Content{Raw: "first"}},
				},
				Next: srv.URL +
					"/repositories/workspace/repo/pullrequests/1/comments?page=2",
			}))
		default:
			assert.NoError(t, json.NewEncoder(w).Encode(CommentList{
				Values: []Comment{
					{ID: 2, Content: Content{Raw: "second"}},
				},
			}))
		}
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)

	var bodies []string
	for comment, err := range gw.ListComments(t.Context(), 1, bitbucket.ListCommentsOptions{}) {
		require.NoError(t, err)
		bodies = append(bodies, comment.Body)
	}
	assert.Equal(t, []string{"first", "second"}, bodies)
}

func TestGateway_ListComments_error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	for _, err := range gw.ListComments(t.Context(), 1, bitbucket.ListCommentsOptions{}) {
		require.Error(t, err)
		assert.ErrorContains(t, err, "list comments")
	}
}

func TestGateway_ResolvableComments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t,
			"/repositories/workspace/repo/pullrequests/1/comments",
			r.URL.Path)
		assert.NoError(t, json.NewEncoder(w).Encode(CommentList{
			Values: []Comment{
				{
					ID:      1,
					Content: Content{Raw: "top-level comment"},
				},
				{
					ID:      2,
					Content: Content{Raw: "unresolved inline"},
					Inline:  &Inline{Path: "main.go"},
				},
				{
					ID:         3,
					Content:    Content{Raw: "resolved inline"},
					Inline:     &Inline{Path: "main.go"},
					Resolution: &Resolution{Type: "comment_resolution"},
				},
			},
		}))
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)

	var got []*bitbucket.ResolvableComment
	for comment, err := range gw.ResolvableComments(t.Context(), 1) {
		require.NoError(t, err)
		got = append(got, comment)
	}

	// Top-level comments are not resolvable and must be skipped,
	// and Pending must always stay false on Bitbucket Cloud.
	assert.Equal(t, []*bitbucket.ResolvableComment{
		{ID: 2, Body: "unresolved inline"},
		{ID: 3, Body: "resolved inline", Resolved: true},
	}, got)
}

func TestGateway_ResolvableComments_error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	for _, err := range gw.ResolvableComments(t.Context(), 1) {
		require.Error(t, err)
		assert.ErrorContains(t, err, "list comments")
	}
}

// SetListChangeCommentsPageSize changes the page size
// used for listing change comments.
//
// It restores the old value after the test finishes.
func SetListChangeCommentsPageSize(t testing.TB, pageSize int) {
	t.Cleanup(stub.Value(&ListChangeCommentsPageSize, pageSize))
}
