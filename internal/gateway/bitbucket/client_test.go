package bitbucket

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
		writeJSON(t, w, http.StatusOK, map[string]any{"uuid": "{captain}"})
	}))
	defer srv.Close()

	client, err := NewClient(
		StaticTokenSource(Token{AccessToken: "test-token"}),
		&ClientOptions{BaseURL: srv.URL},
	)
	require.NoError(t, err)

	_, _, err = client.WorkspaceMemberList(t.Context(), "engineering", nil)
	require.NoError(t, err)
}

func TestNewClient_nilTokenSource(t *testing.T) {
	client, err := NewClient(nil, nil)
	require.Nil(t, client)
	assert.EqualError(t, err, "nil token source")
}

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
		wantErr string
	}{
		{
			name:    "Default",
			baseURL: "",
			want:    "https://api.bitbucket.org/2.0",
		},
		{
			name:    "TrimTrailingSlash",
			baseURL: "https://bitbucket.example.com/2.0/",
			want:    "https://bitbucket.example.com/2.0",
		},
		{
			name:    "DropQueryAndFragment",
			baseURL: "https://bitbucket.example.com/2.0?debug=1#section",
			want:    "https://bitbucket.example.com/2.0",
		},
		{
			name:    "MissingSchemeFails",
			baseURL: "bitbucket.example.com/2.0",
			wantErr: `invalid base URL "bitbucket.example.com/2.0"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, _, err := normalizeBaseURL(tt.baseURL)
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

func TestClient_errors(t *testing.T) {
	t.Run("NotFound", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer srv.Close()

		client := newTestClient(t, srv)
		_, _, err := client.PullRequestGet(t.Context(), "engineering", "warp-core", 55)
		require.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("DestinationBranchNotFound", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, http.StatusBadRequest, map[string]any{
				"type": "error",
				"error": map[string]any{
					"message": "destination branch not found",
				},
			})
		}))
		defer srv.Close()

		client := newTestClient(t, srv)
		_, _, err := client.PullRequestCreate(t.Context(), "engineering", "warp-core", &PullRequestCreateRequest{
			Title: "Refit",
			Source: BranchRef{
				Branch: Branch{Name: "feature"},
			},
			Destination: BranchRef{
				Branch: Branch{Name: "missing"},
			},
		})
		require.ErrorIs(t, err, ErrDestinationBranchNotFound)
	})
}

func TestClient_absoluteNextURL(t *testing.T) {
	var srv *httptest.Server
	var requests []string
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.String())
		switch r.URL.RawQuery {
		case "pagelen=10":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"values": []map[string]any{
					{"id": 1, "title": "One"},
				},
				"next": srv.URL + "/2.0/repositories/engineering/warp-core/pullrequests?page=2",
			})
		case "page=2":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"values": []map[string]any{
					{"id": 2, "title": "Two"},
				},
			})
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
	}))
	defer srv.Close()

	client, err := NewClient(
		StaticTokenSource(Token{AccessToken: "test-token"}),
		&ClientOptions{BaseURL: srv.URL + "/2.0"},
	)
	require.NoError(t, err)

	_, resp, err := client.PullRequestList(
		t.Context(),
		"engineering",
		"warp-core",
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(
		t,
		srv.URL+"/2.0/repositories/engineering/warp-core/pullrequests?page=2",
		resp.NextURL,
	)

	_, _, err = client.PullRequestList(
		t.Context(),
		"engineering",
		"warp-core",
		&PullRequestListOptions{PageURL: resp.NextURL},
	)
	require.NoError(t, err)

	require.Len(t, requests, 2)
	assert.Equal(
		t,
		"/2.0/repositories/engineering/warp-core/pullrequests?pagelen=10",
		requests[0],
	)
	assert.Equal(t, "/2.0/repositories/engineering/warp-core/pullrequests?page=2", requests[1])
}

func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()

	client, err := NewClient(
		StaticTokenSource(Token{AccessToken: "test-token"}),
		&ClientOptions{BaseURL: srv.URL + "/2.0"},
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

func assertJSONBody(t *testing.T, r *http.Request, want string) {
	t.Helper()

	var body any
	require.NoError(t, json.NewDecoder(r.Body).Decode(&body))

	got, err := json.Marshal(body)
	require.NoError(t, err)
	assert.JSONEq(t, want, string(got))
}
