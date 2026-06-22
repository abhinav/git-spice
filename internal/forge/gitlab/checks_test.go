package gitlab

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/gitlab"
)

func TestRepository_ChangeChecks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)

		switch r.URL.Path {
		case "/api/v4/projects/42/merge_requests/55":
			assert.Empty(t, r.URL.RawQuery)
			writeJSON(t, w, gitlab.MergeRequest{
				BasicMergeRequest: gitlab.BasicMergeRequest{
					SHA:          "abc123",
					SourceBranch: "feature/refit",
				},
			})
		case "/api/v4/projects/42/repository/commits/abc123/statuses":
			assert.Equal(t, "100", r.URL.Query().Get("per_page"))
			assert.Equal(t, "feature/refit", r.URL.Query().Get("ref"))
			writeJSON(t, w, []*gitlab.CommitStatus{
				{Name: "unit", Status: gitlab.PipelineStatusSuccess},
				{Name: "lint", Status: gitlab.PipelineStatusFailed},
				{Name: "deploy", Status: gitlab.PipelineStatusManual},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client, err := gitlab.NewClient(
		gitlab.StaticTokenSource(gitlab.Token{
			Type:  gitlab.TokenTypePrivateToken,
			Value: "token",
		}),
		&gitlab.ClientOptions{
			BaseURL:    srv.URL,
			HTTPClient: srv.Client(),
		},
	)
	require.NoError(t, err)

	checks, err := (&Repository{
		client: client,
		repoID: 42,
	}).ChangeChecks(t.Context(), &MR{Number: 55})
	require.NoError(t, err)
	assert.Equal(t, []forge.ChangeCheck{
		{Name: "unit", State: forge.ChangeCheckPassed},
		{Name: "lint", State: forge.ChangeCheckFailed},
		{Name: "deploy", State: forge.ChangeCheckPending},
	}, checks)
}

func TestRepository_ChangeChecks_noHeadSHA(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v4/projects/42/merge_requests/55", r.URL.Path)
		writeJSON(t, w, gitlab.MergeRequest{})
	}))
	defer srv.Close()

	client, err := gitlab.NewClient(
		gitlab.StaticTokenSource(gitlab.Token{
			Type:  gitlab.TokenTypePrivateToken,
			Value: "token",
		}),
		&gitlab.ClientOptions{
			BaseURL:    srv.URL,
			HTTPClient: srv.Client(),
		},
	)
	require.NoError(t, err)

	checks, err := (&Repository{
		client: client,
		repoID: 42,
	}).ChangeChecks(t.Context(), &MR{Number: 55})
	require.NoError(t, err)
	assert.Empty(t, checks)
}

func TestRepository_ChangeChecks_readsCommitStatusesWithoutHeadPipeline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)

		switch r.URL.Path {
		case "/api/v4/projects/42/merge_requests/55":
			assert.Empty(t, r.URL.RawQuery)
			writeJSON(t, w, gitlab.MergeRequest{
				BasicMergeRequest: gitlab.BasicMergeRequest{
					SHA:          "abc123",
					SourceBranch: "feature/refit",
				},
			})
		case "/api/v4/projects/42/repository/commits/abc123/statuses":
			assert.Equal(t, "feature/refit", r.URL.Query().Get("ref"))
			assert.Equal(t, "100", r.URL.Query().Get("per_page"))
			writeJSON(t, w, []*gitlab.CommitStatus{
				{Name: "external", Status: gitlab.PipelineStatusSuccess},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client, err := gitlab.NewClient(
		gitlab.StaticTokenSource(gitlab.Token{
			Type:  gitlab.TokenTypePrivateToken,
			Value: "token",
		}),
		&gitlab.ClientOptions{
			BaseURL:    srv.URL,
			HTTPClient: srv.Client(),
		},
	)
	require.NoError(t, err)

	checks, err := (&Repository{
		client: client,
		repoID: 42,
	}).ChangeChecks(t.Context(), &MR{Number: 55})
	require.NoError(t, err)
	assert.Equal(t, []forge.ChangeCheck{
		{Name: "external", State: forge.ChangeCheckPassed},
	}, checks)
}

func TestCommitStatusCheck(t *testing.T) {
	tests := []struct {
		name string
		give *gitlab.CommitStatus
		want forge.ChangeCheck
	}{
		{
			name: "Success",
			give: &gitlab.CommitStatus{Name: "unit", Status: gitlab.PipelineStatusSuccess},
			want: forge.ChangeCheck{
				Name:  "unit",
				State: forge.ChangeCheckPassed,
			},
		},
		{
			name: "Running",
			give: &gitlab.CommitStatus{Name: "unit", Status: gitlab.PipelineStatusRunning},
			want: forge.ChangeCheck{
				Name:  "unit",
				State: forge.ChangeCheckPending,
			},
		},
		{
			name: "Failed",
			give: &gitlab.CommitStatus{Name: "unit", Status: gitlab.PipelineStatusFailed},
			want: forge.ChangeCheck{
				Name:  "unit",
				State: forge.ChangeCheckFailed,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, commitStatusCheck(tt.give))
		})
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(v))
}
