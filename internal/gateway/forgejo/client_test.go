package forgejo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient_authMethods(t *testing.T) {
	tests := []struct {
		name      string
		token     Token
		wantValue string
	}{
		{
			name:      "APIToken",
			token:     Token{Type: TokenTypeAPIToken, Value: "api-token"},
			wantValue: "token api-token",
		},
		{
			name:      "OAuth2",
			token:     Token{Type: TokenTypeBearer, Value: "oauth2-token"},
			wantValue: "bearer oauth2-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/v1/user", r.URL.Path)
				assert.Equal(t, tt.wantValue, r.Header.Get("Authorization"))
				assert.Equal(t, "git-spice", r.Header.Get("User-Agent"))
				writeJSON(t, w, http.StatusOK, User{
					ID:    1,
					Login: "contributor",
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
			assert.Equal(t, "contributor", user.Login)
		})
	}
}

func TestBuildAuthHeader(t *testing.T) {
	tests := []struct {
		name      string
		token     Token
		wantValue string
	}{
		{
			name:      "APIToken",
			token:     Token{Type: TokenTypeAPIToken, Value: "api-token"},
			wantValue: "token api-token",
		},
		{
			name:      "Bearer",
			token:     Token{Type: TokenTypeBearer, Value: "oauth2-token"},
			wantValue: "bearer oauth2-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header, err := buildAuthHeader(StaticTokenSource(tt.token))
			require.NoError(t, err)

			got, err := header(t.Context())
			require.NoError(t, err)
			assert.Equal(t, tt.wantValue, got.Get("Authorization"))
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
			want:    "https://codeberg.org/api/v1/",
		},
		{
			name:    "HostURLAppendsAPIRoot",
			baseURL: "https://forgejo.example.com",
			want:    "https://forgejo.example.com/api/v1/",
		},
		{
			name:    "HostURLWithPathAppendsAPIRoot",
			baseURL: "https://forgejo.example.com/custom",
			want:    "https://forgejo.example.com/custom/api/v1/",
		},
		{
			name:    "APIURLWithoutTrailingSlashNormalizes",
			baseURL: "https://forgejo.example.com/api/v1",
			want:    "https://forgejo.example.com/api/v1/",
		},
		{
			name:    "APIURLWithTrailingSlashPreserved",
			baseURL: "https://forgejo.example.com/api/v1/",
			want:    "https://forgejo.example.com/api/v1/",
		},
		{
			name:    "QueryAndFragmentAreDropped",
			baseURL: "https://forgejo.example.com/custom?debug=1#frag",
			want:    "https://forgejo.example.com/custom/api/v1/",
		},
		{
			name:    "MissingSchemeFails",
			baseURL: "forgejo.example.com",
			wantErr: `invalid base URL "forgejo.example.com"`,
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

func TestClient_errors(t *testing.T) {
	t.Run("NotFound", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer srv.Close()

		client := newTestClient(t, srv)
		_, _, err := client.PullRequestGet(t.Context(), "owner", "repo", 55)
		require.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("ErrorMessage", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, http.StatusBadRequest, map[string]any{
				"message": "branch cannot be merged",
			})
		}))
		defer srv.Close()

		client := newTestClient(t, srv)
		_, _, err := client.PullRequestMerge(
			t.Context(),
			"owner",
			"repo",
			55,
			&MergePullRequestOption{Do: "merge"},
		)
		require.Error(t, err)

		var respErr *APIError
		require.ErrorAs(t, err, &respErr)
		assert.Equal(t, "branch cannot be merged", respErr.Message)
		assert.Contains(t, respErr.Error(), "400")
		assert.Contains(t, respErr.Error(), "branch cannot be merged")
	})
}

func TestNewResponse_pagination(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Link": []string{
				`<https://forgejo.example.com/api/v1/repos/o/r/pulls?page=2&limit=1>; rel="next"`,
			},
			"X-Total-Count": []string{"3"},
		},
	}

	got := newResponse(resp)
	assert.Equal(t, http.StatusOK, got.StatusCode)
	assert.Equal(t, 2, got.NextPage)
	assert.Equal(t, 3, got.TotalItems)
}

func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()

	client, err := NewClient(
		StaticTokenSource(Token{
			Type:  TokenTypeAPIToken,
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
