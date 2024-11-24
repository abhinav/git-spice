package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
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

func TestLoadAuthenticationTokenOldFormat(t *testing.T) {
	f := Forge{
		Log: log.New(io.Discard),
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
	}).Authenticate(context.Background(), &ui.FileView{W: io.Discard})
	require.NoError(t, err)

	assert.Equal(t, "my-token", tok.AccessToken)
	assert.False(t, tok.GitHubCLI)
}

func TestSelectAuthenticator(t *testing.T) {
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
		auth authenticator
		err  error
	}
	resultc := make(chan result, 1)
	go func() {
		defer close(resultc)

		got, err := selectAuthenticator(view, authenticatorOptions{
			Endpoint: oauth2.Endpoint{},
		})
		resultc <- result{got, err}
	}()

	// TODO: Generalize termtest and use that here
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		assert.Contains(t,
			termtest.Screen(term.Screen),
			"Select an authentication method",
		)
	}, time.Second, 50*time.Millisecond)

	// Go through all options, roll back around to the first, and select it
	for range _authenticationMethods {
		_, _ = io.WriteString(stdinW, "\x1b[B") // Down arrow
	}
	_, _ = io.WriteString(stdinW, "\r") // Enter

	select {
	case res, ok := <-resultc:
		require.True(t, ok)
		auth, err := res.auth, res.err
		require.NoError(t, err)

		_, ok = auth.(*DeviceFlowAuthenticator)
		require.True(t, ok, "want *github.DeviceFlowAuthenticator, got %T", auth)

	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
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
		require.True(t, ok, "want *github.AuthenticationToken, got %T", tok)
		assert.Equal(t, "token", ght.AccessToken)

	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestAuthCLI(t *testing.T) {
	discardView := &ui.FileView{W: io.Discard}

	t.Run("success", func(t *testing.T) {
		tok, err := (&CLIAuthenticator{
			GH: "gh",
			runCmd: func(*exec.Cmd) error {
				return nil
			},
		}).Authenticate(context.Background(), discardView)
		require.NoError(t, err)

		f := Forge{
			Log: log.New(io.Discard),
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
		}).Authenticate(context.Background(), discardView)
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
		}).Authenticate(context.Background(), discardView)
		require.Error(t, err)
		assert.ErrorContains(t, err, "gh not found")
	})
}
