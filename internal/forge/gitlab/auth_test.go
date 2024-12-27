package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/rogpeppe/go-internal/testscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/uitest"
	"go.abhg.dev/testing/stub"
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

	t.Run("NoAccessToken", func(t *testing.T) {
		t.Run("PAT", func(t *testing.T) {
			err := f.SaveAuthenticationToken(&stash, &AuthenticationToken{
				AuthType: AuthTypePAT,
			})
			require.Error(t, err)
			assert.ErrorContains(t, err, "access token is required")
		})

		t.Run("OAuth2", func(t *testing.T) {
			err := f.SaveAuthenticationToken(&stash, &AuthenticationToken{
				AuthType: AuthTypeOAuth2,
			})
			require.Error(t, err)
			assert.ErrorContains(t, err, "access token is required")
		})
	})

	t.Run("CLI", func(t *testing.T) {
		t.Run("MissingHostname", func(t *testing.T) {
			err := f.SaveAuthenticationToken(&stash, &AuthenticationToken{
				AuthType: AuthTypeGitLabCLI,
			})
			require.Error(t, err)
			assert.ErrorContains(t, err, "hostname is required")
		})

		t.Run("UnexpectedAccessToken", func(t *testing.T) {
			err := f.SaveAuthenticationToken(&stash, &AuthenticationToken{
				AccessToken: "access-token",
				Hostname:    "example.com",
				AuthType:    AuthTypeGitLabCLI,
			})
			require.Error(t, err)
			assert.ErrorContains(t, err, "access token must not be set")
		})
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
	for _, typ := range []AuthType{AuthTypePAT, AuthTypeOAuth2, AuthTypeGitLabCLI} {
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
			{AuthTypeGitLabCLI, "GitLab CLI"},
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

func TestSelectAuthenticator(t *testing.T) {
	// Available authentication methods are affected by whether "glab"
	// CLI is available.
	stub.Func(&_execLookPath, "glab", nil)

	uitest.RunScripts(t, func(t testing.TB, ts *testscript.TestScript, view ui.InteractiveView) {
		wantType := strings.TrimSpace(ts.ReadFile("want_type"))

		auth, err := selectAuthenticator(view, authenticatorOptions{
			Endpoint: oauth2.Endpoint{},
			ClientID: _oauthAppID,
			Hostname: "https://gitlab.com",
		})
		require.NoError(t, err)
		assert.Equal(t, wantType, reflect.TypeOf(auth).String())
	}, &uitest.RunScriptsOptions{
		Update: *UpdateFixtures,
	}, "testdata/auth/select_oauth.txt")
}

func TestAuthenticationFlow_PAT(t *testing.T) {
	uitest.RunScripts(t, func(t testing.TB, ts *testscript.TestScript, view ui.InteractiveView) {
		wantToken := strings.TrimSpace(ts.ReadFile("want_token"))

		got, err := new(Forge).AuthenticationFlow(context.Background(), view)
		require.NoError(t, err)

		assert.Equal(t, &AuthenticationToken{
			AuthType:    AuthTypePAT,
			AccessToken: wantToken,
		}, got)
	}, &uitest.RunScriptsOptions{
		Update: *UpdateFixtures,
	}, "testdata/auth/pat.txt")
}

func TestGLabCLI(t *testing.T) {
	glCLI := newGitLabCLI("false") // don't run real CLI

	// False will fail.
	t.Run("Status/Error", func(t *testing.T) {
		ok, err := glCLI.Status(context.Background(), "example.com")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("Token/Error", func(t *testing.T) {
		_, err := glCLI.Token(context.Background(), "example.com")
		require.Error(t, err)
	})

	t.Run("Status/Okay", func(t *testing.T) {
		glCLI.runCmd = func(*exec.Cmd) error { return nil }

		ok, err := glCLI.Status(context.Background(), "example.com")
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("Token/Okay", func(t *testing.T) {
		glCLI.runCmd = func(cmd *exec.Cmd) error {
			_, _ = io.WriteString(cmd.Stderr, "gitlab.com\n")
			_, _ = io.WriteString(cmd.Stderr, "   ✓ Logged in to gitlab.com\n")
			_, _ = io.WriteString(cmd.Stderr, "   ✓ Git operations will use ssh protocol\n")
			_, _ = io.WriteString(cmd.Stderr, "   ✓ Token: 1234567890abcdef\n")
			return nil
		}

		token, err := glCLI.Token(context.Background(), "example.com")
		require.NoError(t, err)
		assert.Equal(t, "1234567890abcdef", token)
	})

	t.Run("Token/NoToken", func(t *testing.T) {
		glCLI.runCmd = func(cmd *exec.Cmd) error {
			_, _ = io.WriteString(cmd.Stderr, "gitlab.com\n")
			_, _ = io.WriteString(cmd.Stderr, "   ✓ Logged in to gitlab.com\n")
			_, _ = io.WriteString(cmd.Stderr, "   ✓ Git operations will use ssh protocol\n")
			return nil
		}

		_, err := glCLI.Token(context.Background(), "example.com")
		require.Error(t, err)
		assert.ErrorContains(t, err, "token not found")
	})
}

func TestURLHostname(t *testing.T) {
	tests := []struct {
		give string
		want string
	}{
		{"https://gitlab.com", "gitlab.com"},
		{"https://gitlab.example.com/api", "gitlab.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			got, err := urlHostname(tt.give)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("BadURL", func(t *testing.T) {
		_, err := urlHostname("://")
		require.Error(t, err)
	})
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

func TestCLIAuthenticator(t *testing.T) {
	var (
		statusOk  bool
		statusErr error
	)

	auth := &CLIAuthenticator{
		Hostname: "example.com",
		CLI: gitlabCLIStub{
			StatusF: func(context.Context, string) (bool, error) {
				return statusOk, statusErr
			},
		},
	}

	ctx := context.Background()
	view := &ui.FileView{W: io.Discard}

	t.Run("Success", func(t *testing.T) {
		statusOk, statusErr = true, nil

		tok, err := auth.Authenticate(ctx, view)
		require.NoError(t, err)
		assert.Equal(t, &AuthenticationToken{
			AuthType: AuthTypeGitLabCLI,
			Hostname: "example.com",
		}, tok)
	})

	t.Run("Unauthenticated", func(t *testing.T) {
		statusOk, statusErr = false, nil

		_, err := auth.Authenticate(ctx, view)
		require.Error(t, err)
		assert.ErrorContains(t, err, "not authenticated")
	})

	t.Run("Error", func(t *testing.T) {
		statusOk, statusErr = false, assert.AnError

		_, err := auth.Authenticate(ctx, view)
		require.Error(t, err)
		assert.ErrorIs(t, err, assert.AnError)
	})
}

type gitlabCLIStub struct {
	TokenF  func(context.Context, string) (string, error)
	StatusF func(context.Context, string) (bool, error)
}

var _ gitlabCLI = gitlabCLIStub{}

func (g gitlabCLIStub) Token(ctx context.Context, hostname string) (string, error) {
	return g.TokenF(ctx, hostname)
}

func (g gitlabCLIStub) Status(ctx context.Context, hostname string) (bool, error) {
	return g.StatusF(ctx, hostname)
}
