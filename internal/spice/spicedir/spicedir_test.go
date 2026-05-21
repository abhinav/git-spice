package spicedir_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/spice/spicedir"
)

func TestPath(t *testing.T) {
	got := spicedir.Path("/repo")
	assert.Equal(t, filepath.Join("/repo", ".spice"), got)
}

func TestResolutionPath(t *testing.T) {
	got := spicedir.ResolutionPath("/repo", "restack")
	assert.Equal(t, filepath.Join("/repo", ".spice", "resolutions", "restack.json"), got)
}

func TestEnsureDir(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, spicedir.EnsureDir(root))

	info, err := os.Stat(filepath.Join(root, ".spice"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Calling again is idempotent.
	require.NoError(t, spicedir.EnsureDir(root))
}

func TestEnsureResolutionsDir(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, spicedir.EnsureResolutionsDir(root))

	info, err := os.Stat(filepath.Join(root, ".spice", "resolutions"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}
