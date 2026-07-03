package cloud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
)

func TestNew_nilToken(t *testing.T) {
	_, err := New(
		"https://api.bitbucket.org/2.0",
		"https://bitbucket.org",
		"workspace", "repo",
		silog.Nop(),
		nil,
		http.DefaultClient,
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "nil authentication token")
}

func TestGateway_Product(t *testing.T) {
	gw := newTestGateway(t, "https://api.bitbucket.org/2.0")
	assert.Equal(t, "Bitbucket", gw.Product())
}

func TestGateway_ChangeURL(t *testing.T) {
	gw := newTestGateway(t, "https://bitbucket.org")
	assert.Equal(t,
		"https://bitbucket.org/workspace/repo/pull-requests/42",
		gw.ChangeURL(42),
	)
}

func TestGateway_ChangeTemplate(t *testing.T) {
	var repositoryGets int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)

		switch r.URL.Path {
		case "/repositories/workspace/repo":
			repositoryGets++
			assert.NoError(t, json.NewEncoder(w).Encode(Repository{
				MainBranch: Branch{Name: "main"},
			}))
		case "/repositories/workspace/repo/src/main/PULL_REQUEST_TEMPLATE.md":
			_, err := w.Write([]byte("## Summary\n"))
			assert.NoError(t, err)
		case "/repositories/workspace/repo/src/main/.bitbucket/pull_request_template.md":
			_, err := w.Write([]byte("nested template\n"))
			assert.NoError(t, err)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)

	body, err := gw.ChangeTemplate(t.Context(), "PULL_REQUEST_TEMPLATE.md")
	require.NoError(t, err)
	assert.Equal(t, "## Summary\n", body)

	body, err = gw.ChangeTemplate(t.Context(), ".bitbucket/pull_request_template.md")
	require.NoError(t, err)
	assert.Equal(t, "nested template\n", body)

	// The default branch lookup is memoized across calls.
	assert.Equal(t, 1, repositoryGets)
}

func TestGateway_ChangeTemplate_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repositories/workspace/repo" {
			assert.NoError(t, json.NewEncoder(w).Encode(Repository{
				MainBranch: Branch{Name: "main"},
			}))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	_, err := gw.ChangeTemplate(t.Context(), "PULL_REQUEST_TEMPLATE.md")
	require.Error(t, err)
	assert.ErrorIs(t, err, forge.ErrNotFound)
}

func TestGateway_ChangeTemplate_emptyRepository(t *testing.T) {
	var repositoryGets int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// An empty repository has no default branch ("mainbranch": null),
		// and must not receive any source file requests.
		assert.Equal(t, "/repositories/workspace/repo", r.URL.Path)
		repositoryGets++
		assert.NoError(t, json.NewEncoder(w).Encode(Repository{}))
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	for range 2 {
		_, err := gw.ChangeTemplate(t.Context(), "PULL_REQUEST_TEMPLATE.md")
		require.Error(t, err)
		assert.ErrorIs(t, err, forge.ErrNotFound)
	}

	// The empty default branch is memoized too,
	// so repeated lookups don't re-fetch the repository.
	assert.Equal(t, 1, repositoryGets)
}

func TestGateway_ChangeTemplate_repositoryError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	_, err := gw.ChangeTemplate(t.Context(), "PULL_REQUEST_TEMPLATE.md")
	require.Error(t, err)
	assert.ErrorContains(t, err, "get default branch")
}

func TestGateway_ChangeTemplate_fileError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repositories/workspace/repo" {
			assert.NoError(t, json.NewEncoder(w).Encode(Repository{
				MainBranch: Branch{Name: "main"},
			}))
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	_, err := gw.ChangeTemplate(t.Context(), "PULL_REQUEST_TEMPLATE.md")
	require.Error(t, err)
	assert.NotErrorIs(t, err, forge.ErrNotFound)
}

func TestGateway_ListCommitChecks(t *testing.T) {
	tests := []struct {
		name     string
		statuses []CommitStatus
		want     []forge.ChangeCheck
	}{
		{
			name: "NoStatuses",
			want: []forge.ChangeCheck{},
		},
		{
			name:     "Successful",
			statuses: []CommitStatus{{Key: "build", State: CommitStatusSuccessful}},
			want: []forge.ChangeCheck{
				{Name: "build", State: forge.ChangeCheckPassed},
			},
		},
		{
			name:     "InProgress",
			statuses: []CommitStatus{{Key: "test", State: CommitStatusInProgress}},
			want: []forge.ChangeCheck{
				{Name: "test", State: forge.ChangeCheckPending},
			},
		},
		{
			name:     "Failed",
			statuses: []CommitStatus{{Key: "lint", State: CommitStatusFailed}},
			want: []forge.ChangeCheck{
				{Name: "lint", State: forge.ChangeCheckFailed},
			},
		},
		{
			name:     "Stopped",
			statuses: []CommitStatus{{Key: "deploy", State: CommitStatusStopped}},
			want: []forge.ChangeCheck{
				{Name: "deploy", State: forge.ChangeCheckFailed},
			},
		},
		{
			name: "Mixed",
			statuses: []CommitStatus{
				{Key: "build", State: CommitStatusSuccessful},
				{Key: "test", State: CommitStatusInProgress},
				{Key: "lint", State: CommitStatusFailed},
			},
			want: []forge.ChangeCheck{
				{Name: "build", State: forge.ChangeCheckPassed},
				{Name: "test", State: forge.ChangeCheckPending},
				{Name: "lint", State: forge.ChangeCheckFailed},
			},
		},
		{
			name:     "UnknownState",
			statuses: []CommitStatus{{State: "WEIRD"}},
			want: []forge.ChangeCheck{
				{
					Name:  "Bitbucket build status 1",
					State: forge.ChangeCheckPassed,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t,
					"/repositories/workspace/repo/commit/abc123/statuses",
					r.URL.Path)
				assert.NoError(t, json.NewEncoder(w).Encode(
					CommitStatusList{Values: tt.statuses}))
			}))
			defer srv.Close()

			gw := newTestGateway(t, srv.URL)
			got, err := gw.ListCommitChecks(t.Context(), git.Hash("abc123"))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGateway_ListCommitChecks_error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	gw := newTestGateway(t, srv.URL)
	_, err := gw.ListCommitChecks(t.Context(), git.Hash("abc123"))
	require.Error(t, err)
	assert.ErrorContains(t, err, "get commit statuses")
}

// newTestGateway builds a Gateway
// that talks to the fake Bitbucket Cloud server at baseURL.
func newTestGateway(t *testing.T, baseURL string) *Gateway {
	t.Helper()

	gw, err := New(
		baseURL,
		baseURL,
		"workspace", "repo",
		silog.Nop(),
		&Token{AccessToken: "test"},
		http.DefaultClient,
	)
	require.NoError(t, err)
	return gw
}
