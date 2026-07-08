package bitbucket

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git/giturl"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/silog"
)

func TestAuthenticationToken_saveLoadClear(t *testing.T) {
	f := &Forge{
		baseURL: "https://bitbucket.example.com",
		Log:     silog.Nop(),
	}
	var stash secret.MemoryStash

	want := &AuthenticationToken{
		AuthType:    AuthTypeAPIToken,
		AccessToken: "secret-token",
	}

	require.NoError(t, f.SaveAuthenticationToken(&stash, want))

	got, err := f.LoadAuthenticationToken(&stash)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	require.NoError(t, f.ClearAuthenticationToken(&stash))

	_, err = f.LoadAuthenticationToken(&stash)
	require.Error(t, err)
	assert.ErrorContains(t, err, "load stored token")
}

func TestAuthenticationToken_keyedByURL(t *testing.T) {
	var stash secret.MemoryStash

	fa := &Forge{baseURL: "https://a.example.com", Log: silog.Nop()}
	fb := &Forge{baseURL: "https://b.example.com", Log: silog.Nop()}

	require.NoError(t, fa.SaveAuthenticationToken(&stash,
		&AuthenticationToken{AccessToken: "tok-a"}))

	_, err := fb.LoadAuthenticationToken(&stash)
	require.Error(t, err)

	got, err := fa.LoadAuthenticationToken(&stash)
	require.NoError(t, err)
	assert.Equal(t, "tok-a", got.(*AuthenticationToken).AccessToken)
}

func TestLoadAuthenticationToken_envPrecedence(t *testing.T) {
	f := &Forge{
		baseURL: "https://bitbucket.example.com",
		token:   "env-token",
		Log:     silog.Nop(),
	}
	var stash secret.MemoryStash

	require.NoError(t, stash.SaveSecret(f.URL(), "token",
		`{"access_token":"stashed-token"}`))

	got, err := f.LoadAuthenticationToken(&stash)
	require.NoError(t, err)

	assert.Equal(t, "env-token", got.(*AuthenticationToken).AccessToken)
}

func TestSaveAuthenticationToken_skipsEnvToken(t *testing.T) {
	f := &Forge{
		baseURL: "https://bitbucket.example.com",
		token:   "env-token",
		Log:     silog.Nop(),
	}
	var stash secret.MemoryStash

	require.NoError(t, f.SaveAuthenticationToken(&stash,
		&AuthenticationToken{AccessToken: "env-token"}))

	_, err := stash.LoadSecret(f.URL(), "token")
	assert.ErrorIs(t, err, secret.ErrNotFound)
}

func TestForge_tokenHelp(t *testing.T) {
	t.Run("Root", func(t *testing.T) {
		f := &Forge{baseURL: "https://bitbucket.example.com"}
		assert.Contains(t, f.tokenHelp(),
			"https://bitbucket.example.com/plugins/servlet/access-tokens/manage")
	})

	t.Run("ContextPath", func(t *testing.T) {
		f := &Forge{baseURL: "https://git.corp.com:8443/bitbucket"}
		assert.Contains(t, f.tokenHelp(),
			"https://git.corp.com:8443/bitbucket/plugins/servlet/access-tokens/manage")
	})
}

func TestForge_validateToken(t *testing.T) {
	t.Run("OK", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
			w.Header().Set("X-AUSERNAME", "jcaptain")
			writeJSON(t, w, http.StatusOK, map[string]any{
				"values":     []any{},
				"isLastPage": true,
			})
		}))
		defer srv.Close()

		f := newForgeForTest(t,
			Options{Kind: KindDataCenter, URL: srv.URL},
			srv.URL+"/scm/KEY/repo.git")
		f.Log = silog.Nop()
		require.NoError(t, f.validateToken(t.Context(),
			&AuthenticationToken{AccessToken: "tok"}))
	})

	t.Run("Unauthorized", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, http.StatusUnauthorized, map[string]any{
				"errors": []map[string]any{
					{"message": "You are not permitted to access this resource."},
				},
			})
		}))
		defer srv.Close()

		f := newForgeForTest(t,
			Options{Kind: KindDataCenter, URL: srv.URL},
			srv.URL+"/scm/KEY/repo.git")
		f.Log = silog.Nop()
		err := f.validateToken(t.Context(),
			&AuthenticationToken{AccessToken: "bad"})
		require.Error(t, err)
	})
}

func TestForge_AuthenticationFlow_missingServerURL(t *testing.T) {
	remoteURL, err := giturl.Parse("https://bitbucket.org/ws/repo.git")
	require.NoError(t, err)

	_, err = (&Definition{
		Log:     silog.Nop(),
		Options: Options{Kind: KindDataCenter},
	}).New(remoteURL)
	require.Error(t, err)
	assert.ErrorContains(t, err, "no Bitbucket Data Center URL configured")
}

func TestForge_AuthenticationFlow_serverAlreadyAuthenticated(t *testing.T) {
	f := newForgeForTest(t,
		Options{
			URL:   "https://bitbucket.example.com",
			Token: "env-token",
		},
		"https://bitbucket.example.com/scm/KEY/repo.git")
	f.Log = silog.Nop()

	_, err := f.AuthenticationFlow(t.Context(), nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "already authenticated")
}

func writeJSON(t *testing.T, w http.ResponseWriter, code int, v any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	require.NoError(t, json.NewEncoder(w).Encode(v))
}
