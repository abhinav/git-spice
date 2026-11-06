package gitea

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
)

func TestRepository_PostChangeComment(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/captain/warp-core/issues/42/comments":
			assertJSONBody(t, r, `{"body":"Navigation stack"}`)
			writeJSON(t, w, http.StatusCreated, giteagw.Comment{ID: 88, Body: "Navigation stack"})
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	id, err := repo.PostChangeComment(t.Context(), &PR{Number: 42}, "Navigation stack")
	require.NoError(t, err)
	assert.Equal(t, "88", id.String())
}

func TestRepository_UpdateChangeComment(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/captain/warp-core/issues/comments/88":
			assertJSONBody(t, r, `{"body":"Updated stack"}`)
			writeJSON(t, w, http.StatusOK, giteagw.Comment{ID: 88, Body: "Updated stack"})
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	err := repo.UpdateChangeComment(t.Context(), &PRComment{ID: 88}, "Updated stack")
	require.NoError(t, err)
}

func TestRepository_DeleteChangeComment(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/captain/warp-core/issues/comments/88":
			assert.Equal(t, http.MethodDelete, r.Method)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	err := repo.DeleteChangeComment(t.Context(), &PRComment{ID: 88})
	require.NoError(t, err)
}

func TestRepository_ListChangeComments_bodyFilter(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/captain/warp-core/issues/42/comments":
			writeJSON(t, w, http.StatusOK, []*giteagw.Comment{
				{ID: 1, Body: "gs-spice navigation marker", User: &giteagw.User{ID: 1, Login: "scotty"}},
				{ID: 2, Body: "unrelated comment", User: &giteagw.User{ID: 2, Login: "spock"}},
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	SetListChangeCommentsPageSize(20)
	repo := newTestRepo(t, srv)

	var items []*forge.ListChangeCommentItem
	for item, err := range repo.ListChangeComments(t.Context(), &PR{Number: 42}, &forge.ListChangeCommentsOptions{
		BodyMatchesAll: []*regexp.Regexp{regexp.MustCompile("gs-spice")},
	}) {
		require.NoError(t, err)
		items = append(items, item)
	}

	require.Len(t, items, 1)
	assert.Equal(t, "1", items[0].ID.String())
	assert.Equal(t, "gs-spice navigation marker", items[0].Body)
}

func TestRepository_ListChangeComments_canUpdate(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/captain/warp-core/issues/42/comments":
			writeJSON(t, w, http.StatusOK, []*giteagw.Comment{
				{ID: 1, Body: "own comment", User: &giteagw.User{ID: 1, Login: "scotty"}},
				{ID: 2, Body: "other comment", User: &giteagw.User{ID: 2, Login: "spock"}},
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	SetListChangeCommentsPageSize(20)
	repo := newTestRepo(t, srv)

	var items []*forge.ListChangeCommentItem
	for item, err := range repo.ListChangeComments(t.Context(), &PR{Number: 42}, &forge.ListChangeCommentsOptions{
		CanUpdate: true,
	}) {
		require.NoError(t, err)
		items = append(items, item)
	}

	// Only the comment from user ID 1 (scotty, the authenticated user) is returned.
	require.Len(t, items, 1)
	assert.Equal(t, "1", items[0].ID.String())
}
