package gitea

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGiteaClient_authMethods(t *testing.T) {
	tests := []struct {
		name       string
		token      Token
		wantHeader string
		wantValue  string
	}{
		{
			name:       "PAT",
			token:      Token{Type: TokenTypeToken, Value: "personal-access-token"},
			wantHeader: "Authorization",
			wantValue:  "token personal-access-token",
		},
		{
			name:       "OAuth2",
			token:      Token{Type: TokenTypeBearer, Value: "oauth2-token"},
			wantHeader: "Authorization",
			wantValue:  "Bearer oauth2-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/v1/user", r.URL.Path)
				assert.Equal(t, tt.wantValue, r.Header.Get(tt.wantHeader))
				assert.Equal(t, "git-spice", r.Header.Get("User-Agent"))
				writeJSON(t, w, http.StatusOK, User{
					ID:    1,
					Login: "kirk",
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
			assert.Equal(t, "kirk", user.Login)
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
			name:    "HostURLAppendsAPIRoot",
			baseURL: "https://gitea.example.com",
			want:    "https://gitea.example.com/api/v1/",
		},
		{
			name:    "HostURLWithPathAppendsAPIRoot",
			baseURL: "https://gitea.example.com/custom",
			want:    "https://gitea.example.com/custom/api/v1/",
		},
		{
			name:    "APIURLWithoutTrailingSlashNormalizes",
			baseURL: "https://gitea.example.com/api/v1",
			want:    "https://gitea.example.com/api/v1/",
		},
		{
			name:    "APIURLWithTrailingSlashPreserved",
			baseURL: "https://gitea.example.com/api/v1/",
			want:    "https://gitea.example.com/api/v1/",
		},
		{
			name:    "QueryAndFragmentAreDropped",
			baseURL: "https://gitea.example.com?debug=1#frag",
			want:    "https://gitea.example.com/api/v1/",
		},
		{
			name:    "MissingSchemeFails",
			baseURL: "gitea.example.com",
			wantErr: `invalid base URL "gitea.example.com"`,
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

func TestNewGiteaClient_emptyBaseURL(t *testing.T) {
	_, err := NewClient(
		StaticTokenSource(Token{Type: TokenTypeToken, Value: "tok"}),
		&ClientOptions{BaseURL: ""},
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "gitea base URL is required")
}

func TestGiteaClient_errors(t *testing.T) {
	t.Run("NotFound", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer srv.Close()

		client := newTestClient(t, srv)
		_, _, err := client.PullGet(t.Context(), "owner", "repo", 42)
		require.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("ErrorMessage", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, http.StatusUnprocessableEntity, map[string]any{
				"message": "warp core unstable",
			})
		}))
		defer srv.Close()

		client := newTestClient(t, srv)
		_, _, err := client.PullCreate(t.Context(), "owner", "repo", &CreatePullRequestOption{
			Title: "fix",
			Head:  "feature",
			Base:  "main",
		})
		require.Error(t, err)

		var respErr *APIError
		require.ErrorAs(t, err, &respErr)
		assert.Equal(t, "warp core unstable", respErr.Message)
		assert.Contains(t, respErr.Error(), "422")
		assert.Contains(t, respErr.Error(), "warp core unstable")
	})
}

func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()

	client, err := NewClient(
		StaticTokenSource(Token{
			Type:  TokenTypeToken,
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
