package gitlab

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vito/midterm"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/termtest"
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

func TestAuthHasGitLabToken(t *testing.T) {
	var logBuffer bytes.Buffer
	f := Forge{
		Options: Options{
			Token: "token",
		},
		Log: log.New(&logBuffer),
	}

	ctx := context.Background()

	t.Run("AuthenticationFlow", func(t *testing.T) {
		_, err := f.AuthenticationFlow(ctx)
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

func TestPATAuthenticator(t *testing.T) {
	stdin, stdinW := io.Pipe()
	defer func() {
		assert.NoError(t, stdinW.Close())
		assert.NoError(t, stdin.Close())
	}()

	term := midterm.NewAutoResizingTerminal()

	type result struct {
		tok forge.AuthenticationToken
		err error
	}
	resultc := make(chan result, 1)
	go func() {
		defer close(resultc)

		got, err := (&PATAuthenticator{
			Stdin:  stdin,
			Stderr: term,
		}).Authenticate(context.Background())
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
