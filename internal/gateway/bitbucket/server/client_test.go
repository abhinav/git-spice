package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient_authHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "git-spice", r.Header.Get("User-Agent"))
		w.Header().Set("X-AUSERNAME", "jcaptain")
		writeJSON(t, w, http.StatusOK, map[string]any{
			"values":     []any{},
			"isLastPage": true,
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	user, _, err := client.CurrentUser(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "jcaptain", user.Name)
}

func TestNewClient_nilTokenSource(t *testing.T) {
	client, err := NewClient(nil, nil)
	require.Nil(t, client)
	assert.EqualError(t, err, "nil token source")
}

func TestNewClient_missingBaseURL(t *testing.T) {
	client, err := NewClient(
		StaticTokenSource(Token{AccessToken: "test-token"}),
		&ClientOptions{},
	)
	require.Nil(t, client)
	assert.ErrorContains(t, err, "base URL is required")
}

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
		wantErr string
	}{
		{
			name:    "RestRoot",
			baseURL: "https://bitbucket.example.com/rest/api/1.0",
			want:    "https://bitbucket.example.com/rest/api/1.0",
		},
		{
			name:    "TrimTrailingSlash",
			baseURL: "https://bitbucket.example.com/rest/api/1.0/",
			want:    "https://bitbucket.example.com/rest/api/1.0",
		},
		{
			name:    "ContextPathPreserved",
			baseURL: "https://example.com/bitbucket/rest/api/1.0",
			want:    "https://example.com/bitbucket/rest/api/1.0",
		},
		{
			name:    "DropQueryAndFragment",
			baseURL: "https://bitbucket.example.com/rest/api/1.0?debug=1#section",
			want:    "https://bitbucket.example.com/rest/api/1.0",
		},
		{
			name:    "Empty",
			baseURL: "",
			wantErr: "base URL is required",
		},
		{
			name:    "MissingSchemeFails",
			baseURL: "bitbucket.example.com/rest/api/1.0",
			wantErr: `invalid base URL "bitbucket.example.com/rest/api/1.0"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeBaseURL(tt.baseURL)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSwapAPIRoot(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		root    string
		want    string
	}{
		{
			name:    "ApiSuffixSwapped",
			baseURL: "https://bitbucket.example.com/rest/api/1.0",
			root:    "/rest/build-status/1.0",
			want:    "https://bitbucket.example.com/rest/build-status/1.0",
		},
		{
			name:    "ContextPathPreserved",
			baseURL: "https://example.com/bitbucket/rest/api/1.0",
			root:    "/rest/default-reviewers/1.0",
			want:    "https://example.com/bitbucket/rest/default-reviewers/1.0",
		},
		{
			name:    "NoApiSuffixAppends",
			baseURL: "https://bitbucket.example.com",
			root:    "/rest/build-status/1.0",
			want:    "https://bitbucket.example.com/rest/build-status/1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, swapAPIRoot(tt.baseURL, tt.root))
		})
	}
}

func TestClient_errors(t *testing.T) {
	t.Run("NotFound", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer srv.Close()

		client := newTestClient(t, srv)
		_, _, err := client.CurrentUser(t.Context())
		require.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("Conflict", func(t *testing.T) {
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
		_, _, err := client.CurrentUser(t.Context())
		require.ErrorIs(t, err, ErrConflict)
	})

	t.Run("EnvelopeParsed", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, http.StatusBadRequest, map[string]any{
				"errors": []map[string]any{
					{
						"message":       "You are not permitted to access this resource.",
						"exceptionName": "com.atlassian.bitbucket.AuthorisationException",
					},
				},
			})
		}))
		defer srv.Close()

		client := newTestClient(t, srv)
		_, _, err := client.CurrentUser(t.Context())
		require.Error(t, err)

		var apiErr *APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
		require.Len(t, apiErr.Details, 1)
		assert.Equal(t,
			"com.atlassian.bitbucket.AuthorisationException",
			apiErr.Details[0].ExceptionName,
		)
		assert.Contains(t, err.Error(), "You are not permitted to access this resource.")
	})

	t.Run("ValidationBadRequest", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, http.StatusBadRequest, map[string]any{
				"errors": []map[string]any{
					{
						"message":       "The branch \"refs/heads/missing\" does not exist.",
						"exceptionName": nil,
					},
				},
			})
		}))
		defer srv.Close()

		client := newTestClient(t, srv)
		_, err := client.post(
			t.Context(),
			"/projects/ENG/repos/warp-core/pull-requests",
			nil,
			map[string]any{"title": "Refit"},
			nil,
		)
		require.Error(t, err)

		var apiErr *APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
		require.Len(t, apiErr.Details, 1)
		assert.Contains(t, err.Error(), "does not exist")
	})
}

func TestClient_buildStatusRouting(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSON(t, w, http.StatusOK, map[string]any{
			"values": []map[string]any{
				{"key": "build-1", "state": BuildStatusSuccessful},
				{"key": "build-2", "state": BuildStatusInProgress},
			},
			"isLastPage": true,
		})
	}))
	defer srv.Close()

	// The build-status base is derived from BaseURL by swapping the
	// /rest/api/1.0 suffix for /rest/build-status/1.0.
	client := newTestClient(t, srv)

	statuses, err := client.BuildStatusList(t.Context(), "abc123")
	require.NoError(t, err)
	require.Len(t, statuses, 2)
	assert.Equal(t, BuildStatusSuccessful, statuses[0].State)
	assert.Equal(t, "/rest/build-status/1.0/commits/abc123", gotPath)
}

func TestClient_buildStatusRouting_multiPage(t *testing.T) {
	// The build-status endpoint is paginated; BuildStatusList must follow
	// isLastPage/nextPageStart to completion rather than truncating to the
	// first page.
	var starts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		starts = append(starts, r.URL.Query().Get("start"))
		switch r.URL.Query().Get("start") {
		case "0", "":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"values": []map[string]any{
					{"key": "build-1", "state": BuildStatusSuccessful},
					{"key": "build-2", "state": BuildStatusInProgress},
				},
				"isLastPage":    false,
				"nextPageStart": 2,
			})
		case "2":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"values": []map[string]any{
					{"key": "build-3", "state": BuildStatusFailed},
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

	statuses, err := client.BuildStatusList(t.Context(), "abc123")
	require.NoError(t, err)
	require.Len(t, statuses, 3)
	assert.Equal(t, BuildStatusFailed, statuses[2].State)
	// Two pages were fetched.
	require.Len(t, starts, 2)
	assert.Equal(t, "0", starts[0])
	assert.Equal(t, "2", starts[1])
}

func TestClient_defaultReviewersRouting(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		// The default-reviewers endpoint responds with a bare JSON array.
		writeJSON(t, w, http.StatusOK, []map[string]any{
			{"name": "alice", "id": 10},
		})
	}))
	defer srv.Close()

	// The default-reviewers base is derived from BaseURL by swapping the
	// /rest/api/1.0 suffix for /rest/default-reviewers/1.0.
	client := newTestClient(t, srv)

	reviewers, _, err := client.DefaultReviewers(
		t.Context(), "ENG", "warp-core", 42, 42,
		"refs/heads/feature", "refs/heads/main",
	)
	require.NoError(t, err)
	require.Len(t, reviewers, 1)
	assert.Equal(t, "alice", reviewers[0].Name)
	assert.Equal(t,
		"/rest/default-reviewers/1.0/projects/ENG/repos/warp-core/reviewers",
		gotPath)
}

func TestClient_CurrentUser_missingHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"values":     []any{},
			"isLastPage": true,
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	_, _, err := client.CurrentUser(t.Context())
	require.Error(t, err)
	assert.ErrorContains(t, err, "X-AUSERNAME")
}

func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()

	client, err := NewClient(
		StaticTokenSource(Token{AccessToken: "test-token"}),
		&ClientOptions{BaseURL: srv.URL + "/rest/api/1.0"},
	)
	require.NoError(t, err)
	return client
}

func writeJSON(t *testing.T, w http.ResponseWriter, code int, v any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	require.NoError(t, json.NewEncoder(w).Encode(v))
}
