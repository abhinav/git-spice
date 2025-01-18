package secret

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/logutil"
)

func TestInsecureStashSaveEmptyDeletesFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "secrets.json")
	stash := InsecureStash{
		Path: file,
		Log:  logutil.TestLogger(t),
	}

	// Delete non-existent secret.
	require.NoError(t, stash.save(&insecureStashData{}))
	assert.NoFileExists(t, file)

	require.NoError(t,
		stash.SaveSecret("service", "key", "secret"))
	assert.FileExists(t, file)

	// Delete existing secret.
	require.NoError(t, stash.DeleteSecret("service", "key"))
	assert.NoFileExists(t, file)
}

func TestInsecureCannotReadOrWrite(t *testing.T) {
	file := filepath.Join(t.TempDir(), "secrets.json")
	// Creating a directory where the file should be
	// will prevent the file from being created.
	require.NoError(t, os.Mkdir(file, 0o700))

	stash := InsecureStash{
		Path: file,
		Log:  logutil.TestLogger(t),
	}

	t.Run("Save", func(t *testing.T) {
		err := stash.SaveSecret("service", "key", "secret")
		require.Error(t, err)
	})

	t.Run("Load", func(t *testing.T) {
		_, err := stash.LoadSecret("service", "key")
		require.Error(t, err)
	})

	t.Run("Delete", func(t *testing.T) {
		err := stash.DeleteSecret("service", "key")
		require.Error(t, err)
	})
}
