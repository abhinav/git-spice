package git

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
	"go.abhg.dev/git-stack/internal/logtest"
)

func TestShell_AddNote(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	require.NoError(t,
		exec.Command("git", "init", "-C", dir).Run())

	shell := Shell{
		WorkDir: dir,
		Logger:  logtest.New(t),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t,
		shell.AddNote(ctx, AddNoteRequest{
			Object:  "HEAD",
			Message: "test",
		}))

	// TODO: read note when we have a way to do that
}
