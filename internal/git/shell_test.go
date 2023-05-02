package git

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
	"go.abhg.dev/git-stack/internal/ioutil"
	"go.abhg.dev/git-stack/internal/logtest"
)

func TestShell_AddNote(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	git(t, "-C", dir, "init")
	git(t, "-C", dir, "commit", "--allow-empty", "-m", "initial commit")

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

func git(t *testing.T, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Stdout = ioutil.TestWriter(t, "stdout:")
	cmd.Stderr = ioutil.TestWriter(t, "stderr:")
	require.NoError(t, cmd.Run())
}
