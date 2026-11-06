package gitea

import (
	"bytes"
	"io"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/hexops/autogold/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/uitest"
)

func TestForge_LoadAuthenticationToken_envVar(t *testing.T) {
	f := &Forge{Options: Options{
		URL:   "https://gitea.example.com",
		Token: "env-token",
	}}

	tok, err := f.LoadAuthenticationToken(new(secret.MemoryStash))
	require.NoError(t, err)

	gTok := tok.(*AuthenticationToken)
	assert.Equal(t, AuthTypeEnvironmentVariable, gTok.AuthType)
	assert.Equal(t, "env-token", gTok.AccessToken)
}

func TestAuth_alreadyHasGiteaToken(t *testing.T) {
	var logBuffer bytes.Buffer
	f := &Forge{
		Options: Options{
			Token: "env-token",
		},
		Log: silog.New(&logBuffer, nil),
	}

	view := ui.NewFileView(io.Discard)

	t.Run("AuthenticationFlow", func(t *testing.T) {
		_, err := f.AuthenticationFlow(t.Context(), view)
		require.Error(t, err)
		assert.ErrorContains(t, err, "already authenticated")
		assert.Contains(t, logBuffer.String(), "Already authenticated")
		assert.Contains(t, logBuffer.String(), "GITEA_TOKEN")
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

func TestAuthenticationFlow_APIToken(t *testing.T) {
	drv := uitest.Drive(t, func(view ui.InteractiveView) {
		got, err := new(Forge).AuthenticationFlow(t.Context(), view)
		require.NoError(t, err)

		assert.Equal(t, &AuthenticationToken{
			AuthType:    AuthTypeAPIToken,
			AccessToken: "my-token",
		}, got)
	}, nil)

	drv.PressN(tea.KeyEnter, 0)
	autogold.Expect("Enter API token:\n").Equal(t, drv.Snapshot())
	drv.Type("my-token")
	drv.Press(tea.KeyEnter)
}

func TestForge_SaveLoadClearToken_PAT(t *testing.T) {
	f := &Forge{Options: Options{URL: "https://gitea.example.com"}}
	stash := new(secret.MemoryStash)

	tok := &AuthenticationToken{
		AuthType:    AuthTypeAPIToken,
		AccessToken: "my-pat",
	}

	require.NoError(t, f.SaveAuthenticationToken(stash, tok))

	loaded, err := f.LoadAuthenticationToken(stash)
	require.NoError(t, err)
	assert.Equal(t, "my-pat", loaded.(*AuthenticationToken).AccessToken)
	assert.Equal(t, AuthTypeAPIToken, loaded.(*AuthenticationToken).AuthType)

	require.NoError(t, f.ClearAuthenticationToken(stash))

	_, err = f.LoadAuthenticationToken(stash)
	require.Error(t, err)
}

func TestForge_SaveAuthenticationToken_skipsEnvVar(t *testing.T) {
	f := &Forge{Options: Options{
		URL:   "https://gitea.example.com",
		Token: "env-token",
	}}
	stash := new(secret.MemoryStash)

	tok := &AuthenticationToken{
		AuthType:    AuthTypeEnvironmentVariable,
		AccessToken: "env-token",
	}

	// When the token matches the env var, SaveAuthenticationToken silently
	// skips persisting it rather than returning an error.
	err := f.SaveAuthenticationToken(stash, tok)
	require.NoError(t, err)

	// Nothing was written to the stash.
	_, loadErr := stash.LoadSecret("https://gitea.example.com", "token")
	require.Error(t, loadErr)
}

func TestGatewayTokenSource_APIToken(t *testing.T) {
	tok := &AuthenticationToken{
		AuthType:    AuthTypeAPIToken,
		AccessToken: "my-pat",
	}
	ts, err := newGatewayTokenSource(tok)
	require.NoError(t, err)

	got, err := ts.Token(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "my-pat", got.Value)
}

func TestGatewayTokenSource_EnvVar(t *testing.T) {
	tok := &AuthenticationToken{
		AuthType:    AuthTypeEnvironmentVariable,
		AccessToken: "env-pat",
	}
	ts, err := newGatewayTokenSource(tok)
	require.NoError(t, err)

	got, err := ts.Token(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "env-pat", got.Value)
}

func TestGatewayTokenSource_nil(t *testing.T) {
	_, err := newGatewayTokenSource(nil)
	require.Error(t, err)
}

// Verify interface at compile time.
var _ forge.AuthenticationToken = (*AuthenticationToken)(nil)
