package bitbucket

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/ui"
)

func TestAPITokenAuthDescription_usesThemeForFocusedURL(t *testing.T) {
	theme := ui.DefaultThemeDark()

	got := apiTokenAuthDescription(theme, true)

	assert.Contains(t, got, "\x1b[")
	assert.Contains(t, ansi.Strip(got), "https://bitbucket.org/account/settings/api-tokens/")
}

func TestLoadAuthenticationToken_noStoredTokenDoesNotFallbackToGCM(
	t *testing.T,
) {
	f := Forge{Log: silog.Nop()}
	var stash secret.MemoryStash

	putFakeGitOnPath(t)

	_, err := f.LoadAuthenticationToken(&stash)
	require.Error(t, err)
	assert.ErrorContains(t, err, "load stored token")
}

func TestLoadAuthenticationToken_storedUseGCM(t *testing.T) {
	f := Forge{Log: silog.Nop()}
	var stash secret.MemoryStash

	putFakeGitOnPath(t)

	err := stash.SaveSecret(
		f.URL(),
		"token",
		`{"auth_type":1}`,
	)
	require.NoError(t, err)

	got, err := f.LoadAuthenticationToken(&stash)
	require.NoError(t, err)
	assert.Equal(
		t,
		&AuthenticationToken{
			AuthType:    AuthTypeGCM,
			AccessToken: "test-token",
		},
		got,
	)
}

func putFakeGitOnPath(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	gitPath := filepath.Join(dir, "git")
	if runtime.GOOS == "windows" {
		gitPath += ".exe"
	}

	testExe, err := os.Executable()
	require.NoError(t, err)
	linkTestBinary(t, testExe, gitPath)

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func linkTestBinary(t *testing.T, testExe, gitPath string) {
	t.Helper()

	if err := os.Symlink(testExe, gitPath); err == nil {
		return
	}

	src, err := os.Open(testExe)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, src.Close())
	}()

	dst, err := os.Create(gitPath)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, dst.Close())
	}()

	_, err = io.Copy(dst, src)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(gitPath, 0o755))
}
