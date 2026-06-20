package bitbucket

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
)

func TestStatusChecks(t *testing.T) {
	tests := []struct {
		name string
		give []bitbucket.CommitStatus
		want []forge.ChangeCheck
	}{
		{
			name: "Empty",
		},
		{
			name: "Mixed",
			give: []bitbucket.CommitStatus{
				{Key: "build", State: bitbucket.CommitStatusSuccessful},
				{Key: "test", State: bitbucket.CommitStatusInProgress},
				{Key: "lint", State: bitbucket.CommitStatusFailed},
			},
			want: []forge.ChangeCheck{
				{Name: "build", State: forge.ChangeCheckPassed},
				{Name: "test", State: forge.ChangeCheckPending},
				{Name: "lint", State: forge.ChangeCheckFailed},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, statusChecks(tt.give))
		})
	}
}

func TestRepository_ChangeChecks_pagesCommitStatuses(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		switch r.URL.String() {
		case "/2.0/repositories/workspace/repo/pullrequests/55":
			writeJSON(t, w, http.StatusOK, bitbucket.PullRequest{
				Source: bitbucket.BranchRef{
					Commit: &bitbucket.Commit{Hash: "abc123"},
				},
			})
		case "/2.0/repositories/workspace/repo/commit/abc123/statuses":
			writeJSON(t, w, http.StatusOK, bitbucket.CommitStatusList{
				Values: []bitbucket.CommitStatus{
					{Key: "build", State: bitbucket.CommitStatusInProgress},
				},
				Next: srv.URL + "/2.0/repositories/workspace/repo/commit/abc123/statuses?page=2",
			})
		case "/2.0/repositories/workspace/repo/commit/abc123/statuses?page=2":
			writeJSON(t, w, http.StatusOK, bitbucket.CommitStatusList{
				Values: []bitbucket.CommitStatus{
					{Key: "test", State: bitbucket.CommitStatusSuccessful},
				},
			})
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
	}))
	defer srv.Close()

	checks, err := newTestRepository(srv.URL+"/2.0").
		ChangeChecks(t.Context(), &PR{Number: 55})
	require.NoError(t, err)
	assert.Equal(t, []forge.ChangeCheck{
		{Name: "build", State: forge.ChangeCheckPending},
		{Name: "test", State: forge.ChangeCheckPassed},
	}, checks)
}

func writeJSON(t *testing.T, w http.ResponseWriter, code int, v any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	require.NoError(t, json.NewEncoder(w).Encode(v))
}
