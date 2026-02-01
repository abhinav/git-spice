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
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hexops/autogold/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/uitest"
	"go.abhg.dev/gs/internal/xec/xectest"
	"go.abhg.dev/testing/stub"
	"go.uber.org/mock/gomock"
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
		Log: silog.New(&logBuffer, nil),
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
		Log: silog.New(&logBuffer, nil),
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

func TestLoadAuthenticationToken_badJSON(t *testing.T) {
	f := Forge{
		Log: silog.Nop(),
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

	drv := uitest.Drive(t, func(view ui.InteractiveView) {
		auth, err := selectAuthenticator(view, authenticatorOptions{
			Endpoint: oauth2.Endpoint{},
			ClientID: _oauthAppID,
			Hostname: "https://gitlab.com",
		})
		require.NoError(t, err)
		assert.True(t, reflect.TypeFor[*DeviceFlowAuthenticator]() == reflect.TypeOf(auth),
			"unexpected authenticator type: %T", auth)
	}, nil)

	drv.Press(tea.KeyDown)
	autogold.Expect(`Select an authentication method:
  OAuth
  Authorize git-spice to act on your behalf from this device only.
  git-spice will get access to all repositories: public and private.
  For private repositories, you will need to request installation from a
  repository owner.

▶ Personal Access Token
  Enter a Personal Access Token generated from https://gitlab.com/-
  /user_settings/personal_access_tokens.
  The Personal Access Token need the following scope: api.

  GitLab CLI
  Re-use an existing GitLab CLI (https://gitlab.com/gitlab-org/cli) session.
  You must be logged into glab with 'glab auth login' for this to work.
  You can use this if you're just experimenting and don't want to set up a
  token yet.
`).Equal(t, drv.Snapshot())

	// Wrap around to OAuth2.
	drv.PressN(tea.KeyDown, 2)
	autogold.Expect(`Select an authentication method:
▶ OAuth
  Authorize git-spice to act on your behalf from this device only.
  git-spice will get access to all repositories: public and private.
  For private repositories, you will need to request installation from a
  repository owner.

  Personal Access Token
  Enter a Personal Access Token generated from https://gitlab.com/-
  /user_settings/personal_access_tokens.
  The Personal Access Token need the following scope: api.

  GitLab CLI
  Re-use an existing GitLab CLI (https://gitlab.com/gitlab-org/cli) session.
  You must be logged into glab with 'glab auth login' for this to work.
  You can use this if you're just experimenting and don't want to set up a
  token yet.
`).Equal(t, drv.Snapshot())
	drv.Press(tea.KeyEnter)
}

func TestAuthenticationFlow_PAT(t *testing.T) {
	drv := uitest.Drive(t, func(view ui.InteractiveView) {
		got, err := new(Forge).AuthenticationFlow(t.Context(), view)
		require.NoError(t, err)

		assert.Equal(t, &AuthenticationToken{
			AuthType:    AuthTypePAT,
			AccessToken: "my-token",
		}, got)
	}, nil)

	drv.Press(tea.KeyDown) // select PAT
	autogold.Expect(`Select an authentication method:
  OAuth
  Authorize git-spice to act on your behalf from this device only.
  git-spice will get access to all repositories: public and private.
  For private repositories, you will need to request installation from a
  repository owner.

▶ Personal Access Token
  Enter a Personal Access Token generated from https://gitlab.com/-
  /user_settings/personal_access_tokens.
  The Personal Access Token need the following scope: api.

  GitLab CLI
  Re-use an existing GitLab CLI (https://gitlab.com/gitlab-org/cli) session.
  You must be logged into glab with 'glab auth login' for this to work.
  You can use this if you're just experimenting and don't want to set up a
  token yet.
`).Equal(t, drv.Snapshot())
	drv.Press(tea.KeyEnter)

	autogold.Expect("Enter Personal Access Token:\n").Equal(t, drv.Snapshot())
	drv.Type("my-token")
	drv.Press(tea.KeyEnter)
}

func TestGLabCLI(t *testing.T) {
	glCLI := newGitLabCLI("false") // don't run real CLI

	// False will fail.
	t.Run("Status/Error", func(t *testing.T) {
		ok, err := glCLI.Status(t.Context(), "example.com")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("Token/Error", func(t *testing.T) {
		_, err := glCLI.Token(t.Context(), "example.com")
		require.Error(t, err)
	})

	execer := xectest.NewMockExecer(gomock.NewController(t))
	glCLI.execer = execer

	t.Run("Status/Okay", func(t *testing.T) {
		execer.EXPECT().
			Run(gomock.Any()).
			Return(nil)

		ok, err := glCLI.Status(t.Context(), "example.com")
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("Token/Okay", func(t *testing.T) {
		execer.EXPECT().
			Run(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) error {
				_, _ = io.WriteString(cmd.Stderr, "gitlab.com\n")
				_, _ = io.WriteString(cmd.Stderr, "   ✓ Logged in to gitlab.com\n")
				_, _ = io.WriteString(cmd.Stderr, "   ✓ Git operations will use ssh protocol\n")
				_, _ = io.WriteString(cmd.Stderr, "   ✓ Token: 1234567890abcdef\n")
				return nil
			})

		token, err := glCLI.Token(t.Context(), "example.com")
		require.NoError(t, err)
		assert.Equal(t, "1234567890abcdef", token)
	})

	t.Run("Token/NoToken", func(t *testing.T) {
		execer.EXPECT().
			Run(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) error {
				_, _ = io.WriteString(cmd.Stderr, "gitlab.com\n")
				_, _ = io.WriteString(cmd.Stderr, "   ✓ Logged in to gitlab.com\n")
				_, _ = io.WriteString(cmd.Stderr, "   ✓ Git operations will use ssh protocol\n")
				return nil
			})

		_, err := glCLI.Token(t.Context(), "example.com")
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
	}).Authenticate(t.Context(), &ui.FileView{W: io.Discard})
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

	view := &ui.FileView{W: io.Discard}

	t.Run("Success", func(t *testing.T) {
		statusOk, statusErr = true, nil

		tok, err := auth.Authenticate(t.Context(), view)
		require.NoError(t, err)
		assert.Equal(t, &AuthenticationToken{
			AuthType: AuthTypeGitLabCLI,
			Hostname: "example.com",
		}, tok)
	})

	t.Run("Unauthenticated", func(t *testing.T) {
		statusOk, statusErr = false, nil

		_, err := auth.Authenticate(t.Context(), view)
		require.Error(t, err)
		assert.ErrorContains(t, err, "not authenticated")
	})

	t.Run("Error", func(t *testing.T) {
		statusOk, statusErr = false, assert.AnError

		_, err := auth.Authenticate(t.Context(), view)
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

func TestCLITokenParsing(t *testing.T) {
	tests := []struct {
		name string
		give string
		want string
	}{
		{
			name: "Plain",
			give: "  ✓ Token: 1234567890abcdef\n",
			want: "1234567890abcdef",
		},
		{
			name: "TokenFound",
			give: "  ✓ Token found: abcdef\n",
			want: "abcdef",
		},
		{
			name: "glabPrefix",
			give: "  ✓ Token: glab-AAAAA\n",
			want: "glab-AAAAA",
		},
		{
			name: "glabPrefixTokenFound",
			give: "  ✓ Token found: glab-AAAAA\n",
			want: "glab-AAAAA",
		},
		{
			name: "Dashes",
			give: "  ✓ Token: abc-def-ghi\n",
			want: "abc-def-ghi",
		},
		{
			name: "NoToken",
			give: "  ✓ Token: \n",
		},
		{
			name: "NoTokenFound",
			give: "  ✓ Token found: \n",
		},
		{
			name: "Unrelated",
			give: "something else\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := _tokenRe.FindSubmatch([]byte(tt.give))
			if len(tt.want) > 0 {
				require.Len(t, m, 2)
				assert.Equal(t, tt.want, string(m[1]))
			} else {
				assert.Len(t, m, 0)
			}
		})
	}
}
