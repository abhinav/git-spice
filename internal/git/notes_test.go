package git

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/ioutil"
)

func TestShell_AddNote(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dir := t.TempDir()
	git(t, "-C", dir, "init")
	git(t, "-C", dir, "commit", "--allow-empty", "-m", "initial commit")

	repo, err := Open(ctx, dir, OpenOptions{})
	require.NoError(t, err)

	notes := repo.Notes("")
	require.NoError(t, notes.Add(ctx, "HEAD", "test", nil))

	got, err := notes.Show(ctx, "HEAD")
	require.NoError(t, err)

	assert.Equal(t, "test", got)
}

func git(t *testing.T, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Stdout = ioutil.TestLogWriter(t, "stdout:")
	cmd.Stderr = ioutil.TestLogWriter(t, "stderr:")
	require.NoError(t, cmd.Run())
}
