package gitea

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
	"go.abhg.dev/gs/internal/silog"
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

func TestRepository_MergeChange_retriesWithExponentialBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var times []time.Time

		client, err := giteagw.NewClient(
			giteagw.StaticTokenSource(giteagw.Token{
				Type:  giteagw.TokenTypeToken,
				Value: "test-token",
			}),
			&giteagw.ClientOptions{
				BaseURL: "https://gitea.example.com",
				HTTPClient: &http.Client{
					Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						require.Equal(t, http.MethodPost, req.Method)
						require.Equal(t,
							"/api/v1/repos/captain/warp-core/pulls/42/merge",
							req.URL.Path)

						times = append(times, time.Now())
						if len(times) < 4 {
							return response(req, http.StatusMethodNotAllowed), nil
						}
						return response(req, http.StatusNoContent), nil
					}),
				},
			},
		)
		require.NoError(t, err)

		repo := &Repository{
			client: client,
			owner:  "captain",
			repo:   "warp-core",
			log:    silog.Nop(),
		}
		err = repo.MergeChange(t.Context(), &PR{Number: 42}, forge.MergeChangeOptions{})
		require.NoError(t, err)

		require.Len(t, times, 4)
		assert.Equal(t, time.Second, times[1].Sub(times[0]))
		assert.Equal(t, 2*time.Second, times[2].Sub(times[1]))
		assert.Equal(t, 4*time.Second, times[3].Sub(times[2]))
	})
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func response(req *http.Request, statusCode int) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("{}")),
		Request:    req,
	}
}
