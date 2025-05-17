package github

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/log"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/uitest"
	"golang.org/x/oauth2"
)

func TestAuthenticationToken_tokenSource(t *testing.T) {
	t.Run("AccessToken", func(t *testing.T) {
		tok := &AuthenticationToken{
			AccessToken: "token",
		}

		src := tok.tokenSource()
		got, err := src.Token()
		require.NoError(t, err)

		assert.Equal(t, "token", got.AccessToken)
	})

	t.Run("GitHubCLI", func(t *testing.T) {
		token := &AuthenticationToken{
			GitHubCLI: true,
		}

		src := token.tokenSource()
		assert.IsType(t, new(CLITokenSource), src)
	})
}

func TestForgeOAuth2Endpoint(t *testing.T) {
	f := Forge{
		Options: Options{
			URL: "https://github.example.com",
		},
	}

	ep, err := f.oauth2Endpoint()
	require.NoError(t, err)
	assert.Equal(t, "https://github.example.com/login/oauth/access_token", ep.TokenURL)
	assert.Equal(t, "https://github.example.com/login/device/code", ep.DeviceAuthURL)

	t.Run("bad URL", func(t *testing.T) {
		f.Options.URL = ":not a valid URL:"
		_, err := f.oauth2Endpoint()
		require.Error(t, err)
	})
}

func TestAuthHasGitHubToken(t *testing.T) {
	var logBuffer bytes.Buffer
	f := Forge{
		Options: Options{
			Token: "token",
		},
		Log: log.New(&logBuffer, nil),
	}

	view := &ui.FileView{W: io.Discard}

	t.Run("AuthenticationFlow", func(t *testing.T) {
		_, err := f.AuthenticationFlow(t.Context(), view)
		require.Error(t, err)
		assert.ErrorContains(t, err, "already authenticated")
		assert.Contains(t, logBuffer.String(), "Already authenticated")
	})

	t.Run("LoadAndSave", func(t *testing.T) {
		var stash secret.MemoryStash
		tok, err := f.LoadAuthenticationToken(&stash)
		require.NoError(t, err)

		err = f.SaveAuthenticationToken(&stash, tok)
		require.NoError(t, err)

		got, err := f.LoadAuthenticationToken(&stash)
		require.NoError(t, err)

		assert.Equal(t, tok, got)

		require.NoError(t, f.ClearAuthenticationToken(&stash))
	})
}

func TestLoadAuthenticationTokenOldFormat(t *testing.T) {
	f := Forge{
		Log: log.Nop(),
	}

	var stash secret.MemoryStash
	require.NoError(t, stash.SaveSecret(f.URL(), "token", "old-token"))

	tok, err := f.LoadAuthenticationToken(&stash)
	require.NoError(t, err)

	assert.Equal(t, "old-token",
		tok.(*AuthenticationToken).AccessToken)
}

func TestDeviceFlowAuthenticator(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /device/code", func(w http.ResponseWriter, r *http.Request) {
		clientID := r.FormValue("client_id")
		if !assert.Equal(t, "client-id", clientID) {
			http.Error(w, "bad client_id", http.StatusBadRequest)
			return
		}

		scope := r.FormValue("scope")
		if !assert.Equal(t, "scope", scope) {
			http.Error(w, "bad scope", http.StatusBadRequest)
			return
		}

		_, _ = w.Write([]byte(`{
			"device_code": "device-code",
			"verification_uri": "https://example.com/verify",
			"expires_in": 900,
			"interval": 1
		}`))
	})

	mux.HandleFunc("POST /oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		clientID := r.FormValue("client_id")
		if !assert.Equal(t, "client-id", clientID) {
			http.Error(w, "bad client_id", http.StatusBadRequest)
			return
		}

		deviceCode := r.FormValue("device_code")
		if !assert.Equal(t, "device-code", deviceCode) {
			http.Error(w, "bad device_code", http.StatusBadRequest)
			return
		}

		result := map[string]string{
			"access_token": "my-token",
			"token_type":   "bearer",
			"scope":        "scope",
		}

		switch r.Header.Get("Accept") {
		case "application/json":
			_ = json.NewEncoder(w).Encode(result)
		default:
			q := make(url.Values)
			for k, v := range result {
				q.Set(k, v)
			}
			_, _ = io.WriteString(w, q.Encode())
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	tok, err := (&DeviceFlowAuthenticator{
		ClientID: "client-id",
		Scopes:   []string{"scope"},
		Endpoint: oauth2.Endpoint{
			DeviceAuthURL: srv.URL + "/device/code",
			TokenURL:      srv.URL + "/oauth/access_token",
		},
	}).Authenticate(t.Context(), &ui.FileView{W: io.Discard})
	require.NoError(t, err)

	assert.Equal(t, "my-token", tok.AccessToken)
	assert.False(t, tok.GitHubCLI)
}

func TestSelectAuthenticator(t *testing.T) {
	uitest.RunScripts(t, func(t testing.TB, ts *testscript.TestScript, view ui.InteractiveView) {
		wantType := strings.TrimSpace(ts.ReadFile("want_type"))

		auth, err := selectAuthenticator(view, authenticatorOptions{
			Endpoint: oauth2.Endpoint{},
		})
		require.NoError(t, err)
		assert.Equal(t, wantType, reflect.TypeOf(auth).String())
	}, &uitest.RunScriptsOptions{
		Update: *UpdateFixtures,
		Rows:   80,
	}, "testdata/auth/select")
}

func TestAuthenticationFlow_PAT(t *testing.T) {
	uitest.RunScripts(t, func(t testing.TB, ts *testscript.TestScript, view ui.InteractiveView) {
		wantToken := strings.TrimSpace(ts.ReadFile("want_token"))

		got, err := new(Forge).AuthenticationFlow(t.Context(), view)
		require.NoError(t, err)

		assert.Equal(t, &AuthenticationToken{
			AccessToken: wantToken,
		}, got)
	}, &uitest.RunScriptsOptions{
		Update: *UpdateFixtures,
		Rows:   80,
	}, "testdata/auth/pat.txt")
}

func TestAuthCLI(t *testing.T) {
	discardView := &ui.FileView{W: io.Discard}

	t.Run("success", func(t *testing.T) {
		tok, err := (&CLIAuthenticator{
			GH: "gh",
			runCmd: func(*exec.Cmd) error {
				return nil
			},
		}).Authenticate(t.Context(), discardView)
		require.NoError(t, err)

		f := Forge{
			Log: log.Nop(),
		}
		var stash secret.MemoryStash
		require.NoError(t, f.SaveAuthenticationToken(&stash, tok))

		t.Run("load", func(t *testing.T) {
			tok, err := f.LoadAuthenticationToken(&stash)
			require.NoError(t, err)

			assert.True(t, tok.(*AuthenticationToken).GitHubCLI)
		})
	})

	t.Run("unauthenticated", func(t *testing.T) {
		_, err := (&CLIAuthenticator{
			GH: "gh",
			runCmd: func(*exec.Cmd) error {
				return &exec.ExitError{
					Stderr: []byte("great sadness"),
				}
			},
		}).Authenticate(t.Context(), discardView)
		require.Error(t, err)
		assert.ErrorContains(t, err, "not authenticated")
		assert.ErrorContains(t, err, "great sadness")
	})

	t.Run("other error", func(t *testing.T) {
		_, err := (&CLIAuthenticator{
			GH: "gh",
			runCmd: func(*exec.Cmd) error {
				return errors.New("gh not found")
			},
		}).Authenticate(t.Context(), discardView)
		require.Error(t, err)
		assert.ErrorContains(t, err, "gh not found")
	})
}
