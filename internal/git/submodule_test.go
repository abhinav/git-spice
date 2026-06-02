package git_test

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestSubmodules(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	log := silogtest.New(t)

	// Set up a submodule repo.
	subDir := t.TempDir()
	initGitRepo(t, subDir)

	// Set up a parent repo with the submodule.
	parentDir := t.TempDir()
	initGitRepo(t, parentDir)
	addSubmodule(t, parentDir, subDir, "libs/core")
	runGit(t, parentDir, "commit", "-m", "add submodule")

	parentWt, err := git.OpenWorktree(ctx, parentDir, git.OpenOptions{
		Log: log,
	})
	require.NoError(t, err)

	t.Run("ListSubmodules", func(t *testing.T) {
		subs, err := parentWt.Submodules(ctx)
		require.NoError(t, err)
		require.Len(t, subs, 1)
		assert.Equal(t, "libs/core", subs[0].Path)
		assert.Equal(t, subDir, subs[0].URL)
	})

	t.Run("SubmoduleCurrentBranch", func(t *testing.T) {
		branch, err := parentWt.SubmoduleCurrentBranch(
			ctx, "libs/core",
		)
		require.NoError(t, err)
		assert.Equal(t, "main", branch)
	})

	t.Run("SubmoduleWorktree", func(t *testing.T) {
		subWt, err := parentWt.SubmoduleWorktree(ctx, "libs/core")
		require.NoError(t, err)
		// Normalize separators: RootDir comes from
		// 'git rev-parse --show-toplevel' which uses forward
		// slashes on all platforms, while filepath.Join uses
		// the platform separator.
		assert.Equal(t,
			filepath.ToSlash(
				filepath.Join(parentWt.RootDir(), "libs", "core"),
			),
			filepath.ToSlash(subWt.RootDir()),
		)
	})
}

func TestSubmodules_noSubmodules(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	initGitRepo(t, dir)

	ctx := t.Context()
	wt, err := git.OpenWorktree(ctx, dir, git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	subs, err := wt.Submodules(ctx)
	require.NoError(t, err)
	assert.Empty(t, subs)
}

func TestSubmoduleCurrentBranch_detachedHead(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	log := silogtest.New(t)

	subDir := t.TempDir()
	initGitRepo(t, subDir)

	parentDir := t.TempDir()
	initGitRepo(t, parentDir)
	addSubmodule(t, parentDir, subDir, "libs/core")
	runGit(t, parentDir, "commit", "-m", "add submodule")

	// Detach submodule HEAD.
	runGit(t, filepath.Join(parentDir, "libs", "core"),
		"checkout", "--detach", "HEAD",
	)

	parentWt, err := git.OpenWorktree(ctx, parentDir, git.OpenOptions{
		Log: log,
	})
	require.NoError(t, err)

	_, err = parentWt.SubmoduleCurrentBranch(ctx, "libs/core")
	assert.ErrorIs(t, err, git.ErrDetachedHead)
}

// initGitRepo creates a bare-minimum git repo with one commit.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()

	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "commit", "--allow-empty", "-m", "init")
}

// addSubmodule adds a local repo as a submodule,
// enabling the file:// transport protocol.
func addSubmodule(t *testing.T, parentDir, subDir, path string) {
	t.Helper()
	cmd := exec.Command("git",
		"-c", "protocol.file.allow=always",
		"submodule", "add", subDir, path,
	)
	cmd.Dir = parentDir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git submodule add: %s", out)
}

// runGit runs a git command in the given directory.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}
