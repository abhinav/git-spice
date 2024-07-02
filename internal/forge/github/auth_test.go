package github_test

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
	"go.abhg.dev/gs/internal/forge/github"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/termtest"
)

func TestAuthHasGitHubToken(t *testing.T) {
	var logBuffer bytes.Buffer
	f := github.Forge{
		Options: github.Options{
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

		got, err := (&github.PATAuthenticator{
			Stdin:  stdin,
			Stderr: term,
		}).Authenticate(context.Background())
		resultc <- result{got, err}
	}()

	// TODO: Would be nice to be able to re-use termtest's
	// scripting functionality here.
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

		ght, ok := tok.(*github.AuthenticationToken)
		require.True(t, ok, "want *github.AuthenticationToken, got %T", tok)
		assert.Equal(t, "token", ght.AccessToken)

	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}
