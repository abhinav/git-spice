package gitlab

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

// Client is a GitLab client exported for testing.
type Client = gitlabClient

func TestNewGitLabClient_authMethods(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		switch r.URL.Path {
		case "/api/v4/user":
			var u gitlab.User
			switch {
			case r.Header.Get("Private-Token") == "personal-access-token":
				u.Username = "pat-user"

			case r.Header.Get("Private-Token") == "pat-from-env":
				u.Username = "pat-from-env-user"

			case r.Header.Get("Authorization") == "Bearer oauth2-token":
				u.Username = "oauth2-user"

			default:
				t.Errorf("unknown request: %v %v", r.Method, r.URL.Path)
				t.Errorf("headers: %v", r.Header)
			}

			assert.NoError(t, enc.Encode(u))

		default:
			t.Errorf("unknown request: %v %v", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	t.Run("PAT", func(t *testing.T) {
		client, err := newGitLabClient(t.Context(), srv.URL, &AuthenticationToken{
			AuthType:    AuthTypePAT,
			AccessToken: "personal-access-token",
		})
		require.NoError(t, err)

		u, _, err := client.Users.CurrentUser(gitlab.WithContext(t.Context()))
		require.NoError(t, err)
		assert.Equal(t, "pat-user", u.Username)
	})

	t.Run("OAuth2", func(t *testing.T) {
		client, err := newGitLabClient(t.Context(), srv.URL, &AuthenticationToken{
			AuthType:    AuthTypeOAuth2,
			AccessToken: "oauth2-token",
		})
		require.NoError(t, err)

		u, _, err := client.Users.CurrentUser(gitlab.WithContext(t.Context()))
		require.NoError(t, err)
		assert.Equal(t, "oauth2-user", u.Username)
	})

	t.Run("EnvironmentVariable", func(t *testing.T) {
		client, err := newGitLabClient(t.Context(), srv.URL, &AuthenticationToken{
			AuthType:    AuthTypeEnvironmentVariable,
			AccessToken: "pat-from-env",
		})
		require.NoError(t, err)

		u, _, err := client.Users.CurrentUser(gitlab.WithContext(t.Context()))
		require.NoError(t, err)
		assert.Equal(t, "pat-from-env-user", u.Username)
	})
}
