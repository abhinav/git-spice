package gitea

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/secret"
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
