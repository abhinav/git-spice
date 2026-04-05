package gitlab

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGitLabClient_authMethods(t *testing.T) {
	tests := []struct {
		name       string
		token      Token
		wantHeader string
		wantValue  string
	}{
		{
			name:       "PAT",
			token:      Token{Type: TokenTypePrivateToken, Value: "personal-access-token"},
			wantHeader: "Private-Token",
			wantValue:  "personal-access-token",
		},
		{
			name:       "OAuth2",
			token:      Token{Type: TokenTypeBearer, Value: "oauth2-token"},
			wantHeader: "Authorization",
			wantValue:  "Bearer oauth2-token",
		},
		{
			name:       "EnvironmentVariable",
			token:      Token{Type: TokenTypePrivateToken, Value: "pat-from-env"},
			wantHeader: "Private-Token",
			wantValue:  "pat-from-env",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/v4/user", r.URL.Path)
				assert.Equal(t, tt.wantValue, r.Header.Get(tt.wantHeader))
				assert.Equal(t, "git-spice", r.Header.Get("User-Agent"))
				writeJSON(t, w, http.StatusOK, User{
					ID:       1,
					Username: "captain",
				})
			}))
			defer srv.Close()

			client, err := NewClient(
				StaticTokenSource(tt.token),
				&ClientOptions{BaseURL: srv.URL},
			)
			require.NoError(t, err)

			user, _, err := client.UserCurrent(t.Context())
			require.NoError(t, err)
			assert.Equal(t, int64(1), user.ID)
			assert.Equal(t, "captain", user.Username)
		})
	}
}

func TestBuildAuthHeader(t *testing.T) {
	tests := []struct {
		name      string
		token     Token
		wantKey   string
		wantValue string
	}{
		{
			name:      "PAT",
			token:     Token{Type: TokenTypePrivateToken, Value: "personal-access-token"},
			wantKey:   "Private-Token",
			wantValue: "personal-access-token",
		},
		{
			name:      "EnvironmentVariable",
			token:     Token{Type: TokenTypePrivateToken, Value: "pat-from-env"},
			wantKey:   "Private-Token",
			wantValue: "pat-from-env",
		},
		{
			name:      "OAuth2",
			token:     Token{Type: TokenTypeBearer, Value: "oauth2-token"},
			wantKey:   "Authorization",
			wantValue: "Bearer oauth2-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header, err := buildAuthHeader(StaticTokenSource(tt.token))
			require.NoError(t, err)

			got, err := header(t.Context())
			require.NoError(t, err)
			assert.Equal(t, tt.wantValue, got.Get(tt.wantKey))
		})
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
		wantErr string
	}{
		{
			name:    "EmptyUsesDefaultHost",
			baseURL: "",
			want:    "https://gitlab.com/api/v4/",
		},
		{
			name:    "HostURLAppendsAPIRoot",
			baseURL: "https://gitlab.example.com",
			want:    "https://gitlab.example.com/api/v4/",
		},
		{
			name:    "HostURLWithPathAppendsAPIRoot",
			baseURL: "https://gitlab.example.com/custom",
			want:    "https://gitlab.example.com/custom/api/v4/",
		},
		{
			name:    "APIURLWithoutTrailingSlashNormalizes",
			baseURL: "https://gitlab.example.com/api/v4",
			want:    "https://gitlab.example.com/api/v4/",
		},
		{
			name:    "APIURLWithTrailingSlashPreserved",
			baseURL: "https://gitlab.example.com/api/v4/",
			want:    "https://gitlab.example.com/api/v4/",
		},
		{
			name:    "QueryAndFragmentAreDropped",
			baseURL: "https://gitlab.example.com/custom?debug=1#frag",
			want:    "https://gitlab.example.com/custom/api/v4/",
		},
		{
			name:    "MissingSchemeFails",
			baseURL: "gitlab.example.com",
			wantErr: `invalid base URL "gitlab.example.com"`,
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

func TestGitLabClient_errors(t *testing.T) {
	t.Run("NotFound", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer srv.Close()

		client := newTestClient(t, srv)
		_, _, err := client.MergeRequestGet(t.Context(), int64(42), 55, nil)
		require.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("ErrorMessage", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, http.StatusBadRequest, map[string]any{
				"message": map[string]any{
					"base": []string{"warp core unstable"},
				},
			})
		}))
		defer srv.Close()

		client := newTestClient(t, srv)
		_, _, err := client.MergeRequestAccept(t.Context(), int64(42), 55, &AcceptMergeRequestOptions{})
		require.Error(t, err)

		var respErr *APIError
		require.ErrorAs(t, err, &respErr)
		assert.Equal(t, "base: warp core unstable", respErr.Message)
		assert.Contains(t, respErr.Error(), "400")
		assert.Contains(t, respErr.Error(), "base: warp core unstable")
	})
}

func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()

	client, err := NewClient(
		StaticTokenSource(Token{
			Type:  TokenTypePrivateToken,
			Value: "test-token",
		}),
		&ClientOptions{BaseURL: srv.URL},
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
