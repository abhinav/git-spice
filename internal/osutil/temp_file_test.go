package osutil

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTempFilePath(t *testing.T) {
	f, err := TempFilePath(t.TempDir(), "tempfile")
	require.NoError(t, err)

	_, err = os.Stat(f)
	require.NoError(t, err)
}

func TestTempFilePath_manyParallel(t *testing.T) {
	const N = 100

	dir := t.TempDir()
	var ready, done sync.WaitGroup
	ready.Add(N)
	done.Add(N)
	gotPaths := make([]string, N)
	for i := range N {
		go func() {
			defer done.Done()

			ready.Done() // I'm ready.
			ready.Wait() // Is everyone else?

			path, err := TempFilePath(dir, "foo")
			assert.NoError(t, err)
			gotPaths[i] = path // no mutex necessary
		}()
	}
	done.Wait()

	// Verify all paths are unique.
	seen := make(map[string]struct{})
	for _, path := range gotPaths {
		_, ok := seen[path]
		assert.False(t, ok, "duplicate path: %s", path)
		seen[path] = struct{}{}
	}
}

func TestTempFilePath_badDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	_, err := TempFilePath(dir, "tempfile")
	require.Error(t, err)
	assert.ErrorIs(t, err, os.ErrNotExist)
}
