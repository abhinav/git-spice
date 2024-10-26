package browsertest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/browser/browsertest"
)

func TestRecorder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "urls")
	rec := browsertest.NewRecorder(path)

	require.NoError(t, rec.OpenURL("https://example.com"))
	require.NoError(t, rec.OpenURL("https://example.org"))

	got, err := os.ReadFile(path)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"https://example.com",
		"https://example.org",
	}, strings.Split(strings.TrimSpace(string(got)), "\n"))
}

func TestRecorder_openFileError(t *testing.T) {
	rec := browsertest.NewRecorder(
		filepath.Join(t.TempDir(), "nonexistent", "file"),
	)

	err := rec.OpenURL("https://example.com")
	assert.Error(t, err)
}
