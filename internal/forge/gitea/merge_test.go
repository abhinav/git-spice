package gitea

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
)

func TestRepository_MergeChange_default(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/captain/warp-core/pulls/42/merge":
			assertJSONBody(t, r, `{"Do":"merge"}`)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	err := repo.MergeChange(t.Context(), &PR{Number: 42}, forge.MergeChangeOptions{})
	require.NoError(t, err)
}

func TestRepository_MergeChange_squash(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/captain/warp-core/pulls/42/merge":
			assertJSONBody(t, r, `{"Do":"squash"}`)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	err := repo.MergeChange(t.Context(), &PR{Number: 42}, forge.MergeChangeOptions{
		Method: forge.MergeMethodSquash,
	})
	require.NoError(t, err)
}

func TestRepository_MergeChange_rebase(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/captain/warp-core/pulls/42/merge":
			assertJSONBody(t, r, `{"Do":"rebase"}`)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	err := repo.MergeChange(t.Context(), &PR{Number: 42}, forge.MergeChangeOptions{
		Method: forge.MergeMethodRebase,
	})
	require.NoError(t, err)
}

func TestRepository_MergeChange_withHeadHash(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/captain/warp-core/pulls/42/merge":
			assertJSONBody(t, r, `{"Do":"merge","head_commit_id":"abc123"}`)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	err := repo.MergeChange(t.Context(), &PR{Number: 42}, forge.MergeChangeOptions{
		HeadHash: "abc123",
	})
	require.NoError(t, err)
}

func TestMergeMethod(t *testing.T) {
	tests := []struct {
		method forge.MergeMethod
		want   string
	}{
		{forge.MergeMethodDefault, "merge"},
		{forge.MergeMethodMerge, "merge"},
		{forge.MergeMethodSquash, "squash"},
		{forge.MergeMethodRebase, "rebase"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, mergeMethod(tt.method), "MergeMethod(%v)", tt.method)
	}
}

// Suppress unused import warning.
var _ = giteagw.User{}
