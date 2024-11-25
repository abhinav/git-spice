package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vito/midterm"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/termtest"
	"go.abhg.dev/gs/internal/ui"
	"golang.org/x/oauth2"
)

func TestForgeOAuth2Endpoint(t *testing.T) {
	t.Run("DefaultURL", func(t *testing.T) {
		var f Forge

		ep, err := f.oauth2Endpoint()
		require.NoError(t, err)

		assert.Equal(t, "https://gitlab.com/oauth/authorize", ep.AuthURL)
	})

	t.Run("CustomURL", func(t *testing.T) {
		f := Forge{
			Options: Options{
				URL: "https://gitlab.example.com",
			},
		}

		ep, err := f.oauth2Endpoint()
		require.NoError(t, err)

		assert.Equal(t, "https://gitlab.example.com/oauth/authorize", ep.AuthURL)
	})

	t.Run("BadURL", func(t *testing.T) {
		f := Forge{
			Options: Options{
				URL: "://",
			},
		}

		_, err := f.oauth2Endpoint()
		require.Error(t, err)
		assert.ErrorContains(t, err, "bad GitLab URL")
	})
}

func TestAuthSaveAndLoad(t *testing.T) {
	var logBuffer bytes.Buffer
	f := Forge{
		Log: log.New(&logBuffer),
	}

	var stash secret.MemoryStash
	t.Run("DoesNotExist", func(t *testing.T) {
		_, err := f.LoadAuthenticationToken(&stash)
		require.Error(t, err)
		assert.ErrorIs(t, err, secret.ErrNotFound)
	})

	require.NoError(t, f.SaveAuthenticationToken(&stash, &AuthenticationToken{
		AccessToken: "token",
		AuthType:    AuthTypePAT,
	}))

	t.Run("Exists", func(t *testing.T) {
		tok, err := f.LoadAuthenticationToken(&stash)
		require.NoError(t, err)

		assert.Equal(t, &AuthenticationToken{
			AccessToken: "token",
			AuthType:    AuthTypePAT,
		}, tok)
	})

	t.Run("CantSaveEnv", func(t *testing.T) {
		err := f.SaveAuthenticationToken(&stash, &AuthenticationToken{
			AccessToken: "foo",
			AuthType:    AuthTypeEnvironmentVariable,
		})
		require.Error(t, err)
	})
}

func TestAuth_alreadyHasGitLabToken(t *testing.T) {
	var logBuffer bytes.Buffer
	f := Forge{
		Options: Options{
			Token: "token",
		},
		Log: log.New(&logBuffer),
	}

	ctx := context.Background()
	view := &ui.FileView{W: io.Discard}

	t.Run("AuthenticationFlow", func(t *testing.T) {
		_, err := f.AuthenticationFlow(ctx, view)
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

func TestLoadAuthenticationToken_badJSON(t *testing.T) {
	f := Forge{
		Log: log.New(io.Discard),
	}

	var stash secret.MemoryStash
	require.NoError(t, stash.SaveSecret(f.URL(), "token", "not valid JSON"))

	_, err := f.LoadAuthenticationToken(&stash)
	require.Error(t, err)
	assert.ErrorContains(t, err, "unmarshal token")
}

func TestAuthType(t *testing.T) {
	for _, typ := range []AuthType{AuthTypePAT, AuthTypeOAuth2} {
		t.Run(typ.String(), func(t *testing.T) {
			t.Run("JSONRoundTrip", func(t *testing.T) {
				bs, err := json.Marshal(typ)
				require.NoError(t, err)

				var got AuthType
				require.NoError(t, json.Unmarshal(bs, &got))

				assert.Equal(t, typ, got)
			})
		})
	}

	t.Run("JSONError", func(t *testing.T) {
		t.Run("AuthTypeEnvironmentVariable", func(t *testing.T) {
			_, err := json.Marshal(AuthTypeEnvironmentVariable)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "should never save")
		})

		t.Run("Unknown", func(t *testing.T) {
			_, err := json.Marshal(AuthType(42))
			require.Error(t, err)

			var got AuthType
			require.Error(t, json.Unmarshal([]byte(`"foo"`), &got))
		})
	})

	t.Run("String", func(t *testing.T) {
		tests := []struct {
			typ AuthType
			str string
		}{
			{AuthTypePAT, "Personal Access Token"},
			{AuthTypeOAuth2, "OAuth2"},
			{AuthTypeEnvironmentVariable, "Environment Variable"},
			{AuthType(42), "AuthType(42)"},
		}

		for _, tt := range tests {
			t.Run(tt.str, func(t *testing.T) {
				assert.Equal(t, tt.str, tt.typ.String())
			})
		}
	})
}

func TestPATAuthenticator(t *testing.T) {
	stdin, stdinW := io.Pipe()
	defer func() {
		assert.NoError(t, stdinW.Close())
		assert.NoError(t, stdin.Close())
	}()

	term := midterm.NewAutoResizingTerminal()
	view := &ui.TerminalView{
		R: stdin,
		W: term,
	}

	type result struct {
		tok forge.AuthenticationToken
		err error
	}
	resultc := make(chan result, 1)
	go func() {
		defer close(resultc)

		got, err := (&PATAuthenticator{}).Authenticate(context.Background(), view)
		resultc <- result{got, err}
	}()

	// TODO: Generalize termtest and use that here
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		assert.Contains(t,
			termtest.Screen(term.Screen),
			"Enter Personal Access Token",
		)
	}, time.Second, 50*time.Millisecond)

	_, _ = io.WriteString(stdinW, "token\r")

	select {
	case res, ok := <-resultc:
		require.True(t, ok)
		tok, err := res.tok, res.err
		require.NoError(t, err)

		ght, ok := tok.(*AuthenticationToken)
		require.True(t, ok, "want *gitlab.AuthenticationToken, got %T", tok)
		assert.Equal(t, "token", ght.AccessToken)

	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestDeviceFlowAuthenticator(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth/authorize_device", func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
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
			DeviceAuthURL: srv.URL + "/oauth/authorize_device",
			TokenURL:      srv.URL + "/oauth/token",
		},
	}).Authenticate(context.Background(), &ui.FileView{W: io.Discard})
	require.NoError(t, err)

	assert.Equal(t, &AuthenticationToken{
		AccessToken: "my-token",
		AuthType:    AuthTypeOAuth2,
	}, tok)
}
