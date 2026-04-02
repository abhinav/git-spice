package submit

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
)

func TestGitEditor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	runGit := func(t *testing.T, dir string, args ...string) {
		t.Helper()

		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		require.NoError(t, cmd.Run())
	}

	t.Run("RepoLocalEditor", func(t *testing.T) {
		repoDir := filepath.Join(t.TempDir(), "repo")
		require.NoError(t, os.Mkdir(repoDir, 0o755))

		runGit(t, repoDir, "init")
		runGit(t, repoDir, "config", "user.name", "Test")
		runGit(t, repoDir, "config", "user.email", "test@example.com")
		runGit(t, repoDir, "commit", "--allow-empty", "-m", "Initial commit")
		runGit(t, repoDir, "config", "core.editor", "repo-editor")
		runGit(t, repoDir, "worktree", "add", "../linked", "-b", "feature")

		worktree, err := git.OpenWorktree(
			t.Context(),
			filepath.Join(filepath.Dir(repoDir), "linked"),
			git.OpenOptions{Log: silog.Nop()},
		)
		require.NoError(t, err)

		assert.Equal(t, "repo-editor", gitEditor(t.Context(), worktree))
	})

	t.Run("WorktreeLocalEditor", func(t *testing.T) {
		repoDir := filepath.Join(t.TempDir(), "repo")
		require.NoError(t, os.Mkdir(repoDir, 0o755))

		runGit(t, repoDir, "init")
		runGit(t, repoDir, "config", "user.name", "Test")
		runGit(t, repoDir, "config", "user.email", "test@example.com")
		runGit(t, repoDir, "commit", "--allow-empty", "-m", "Initial commit")
		runGit(t, repoDir, "config", "extensions.worktreeConfig", "true")
		runGit(t, repoDir, "config", "core.editor", "repo-editor")
		runGit(t, repoDir, "worktree", "add", "../linked", "-b", "feature")
		runGit(
			t,
			filepath.Join(filepath.Dir(repoDir), "linked"),
			"config",
			"--worktree",
			"core.editor",
			"worktree-editor",
		)

		worktree, err := git.OpenWorktree(
			t.Context(),
			filepath.Join(filepath.Dir(repoDir), "linked"),
			git.OpenOptions{Log: silog.Nop()},
		)
		require.NoError(t, err)

		assert.Equal(t, "worktree-editor", gitEditor(t.Context(), worktree))
	})

	t.Run("UserConfigFallback", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

		repoDir := filepath.Join(t.TempDir(), "repo")
		require.NoError(t, os.Mkdir(repoDir, 0o755))

		runGit(t, repoDir, "init")
		runGit(t, repoDir, "config", "--global", "core.editor", "user-editor")

		worktree, err := git.OpenWorktree(
			t.Context(),
			repoDir,
			git.OpenOptions{Log: silog.Nop()},
		)
		require.NoError(t, err)

		assert.Equal(t, "user-editor", gitEditor(t.Context(), worktree))
	})

	t.Run("EnvFallback", func(t *testing.T) {
		t.Setenv("EDITOR", "env-editor")

		repoDir := filepath.Join(t.TempDir(), "repo")
		require.NoError(t, os.Mkdir(repoDir, 0o755))

		runGit(t, repoDir, "init")

		worktree, err := git.OpenWorktree(
			t.Context(),
			repoDir,
			git.OpenOptions{Log: silog.Nop()},
		)
		require.NoError(t, err)

		assert.Equal(t, "env-editor", gitEditor(t.Context(), worktree))
	})
}
