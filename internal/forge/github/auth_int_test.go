package github

import (
	"context"
	"io"
	"os/exec"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/secret"
)

func TestAuthCLI(t *testing.T) {
	f := Forge{
		Log: log.New(io.Discard),
	}
	var stash secret.MemoryStash

	{
		tok, err := (&CLIAuthenticator{
			GH: "gh",
			runCmd: func(*exec.Cmd) error {
				return nil
			},
		}).Authenticate(context.Background())
		require.NoError(t, err)
		require.NoError(t, f.SaveAuthenticationToken(&stash, tok))
	}

	tok, err := f.LoadAuthenticationToken(&stash)
	require.NoError(t, err)

	assert.True(t, tok.(*AuthenticationToken).GitHubCLI)
}
