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
		assert.Equal(t,
			filepath.Join(parentWt.RootDir(), "libs", "core"),
			subWt.RootDir(),
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

func TestSubmoduleHead(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	log := silogtest.New(t)

	subDir := t.TempDir()
	initGitRepo(t, subDir)

	parentDir := t.TempDir()
	initGitRepo(t, parentDir)
	addSubmodule(t, parentDir, subDir, "libs/core")
	runGit(t, parentDir, "commit", "-m", "add submodule")

	parentWt, err := git.OpenWorktree(ctx, parentDir, git.OpenOptions{
		Log: log,
	})
	require.NoError(t, err)

	got, err := parentWt.SubmoduleHead(ctx, "libs/core")
	require.NoError(t, err)
	assert.NotEmpty(t, got, "expected non-empty hash")
}

func TestSubmoduleGitlink(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	log := silogtest.New(t)

	subDir := t.TempDir()
	initGitRepo(t, subDir)

	parentDir := t.TempDir()
	initGitRepo(t, parentDir)
	addSubmodule(t, parentDir, subDir, "libs/core")
	runGit(t, parentDir, "commit", "-m", "add submodule")

	parentWt, err := git.OpenWorktree(ctx, parentDir, git.OpenOptions{
		Log: log,
	})
	require.NoError(t, err)

	// Gitlink at HEAD should match the submodule's HEAD at the
	// time the submodule was added.
	gitlink, err := parentWt.SubmoduleGitlink(ctx, "libs/core")
	require.NoError(t, err)

	subHead, err := parentWt.SubmoduleHead(ctx, "libs/core")
	require.NoError(t, err)

	assert.Equal(t, subHead, gitlink,
		"gitlink should match submodule HEAD right after add")
}

func TestSubmoduleHasGsStore(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	log := silogtest.New(t)

	subDir := t.TempDir()
	initGitRepo(t, subDir)

	parentDir := t.TempDir()
	initGitRepo(t, parentDir)
	addSubmodule(t, parentDir, subDir, "libs/core")
	runGit(t, parentDir, "commit", "-m", "add submodule")

	parentWt, err := git.OpenWorktree(ctx, parentDir, git.OpenOptions{
		Log: log,
	})
	require.NoError(t, err)

	t.Run("Uninitialized", func(t *testing.T) {
		has, err := parentWt.SubmoduleHasGsStore(ctx, "libs/core")
		require.NoError(t, err)
		assert.False(t, has)
	})

	t.Run("Initialized", func(t *testing.T) {
		// Simulate gs init by creating the spice data ref.
		runGit(t, filepath.Join(parentDir, "libs", "core"),
			"update-ref", "refs/spice/data", "HEAD",
		)
		has, err := parentWt.SubmoduleHasGsStore(ctx, "libs/core")
		require.NoError(t, err)
		assert.True(t, has)
	})
}

func TestSubmoduleStatus(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	log := silogtest.New(t)

	subDir := t.TempDir()
	initGitRepo(t, subDir)

	parentDir := t.TempDir()
	initGitRepo(t, parentDir)
	addSubmodule(t, parentDir, subDir, "libs/core")
	runGit(t, parentDir, "commit", "-m", "add submodule")

	parentWt, err := git.OpenWorktree(ctx, parentDir, git.OpenOptions{
		Log: log,
	})
	require.NoError(t, err)

	t.Run("OnBranch", func(t *testing.T) {
		status, err := parentWt.SubmoduleStatus(ctx, "libs/core")
		require.NoError(t, err)
		assert.Equal(t, "libs/core", status.Path)
		assert.Equal(t, "main", status.Branch)
		assert.False(t, status.Detached)
		assert.NotEmpty(t, status.HeadHash)
		assert.NotEmpty(t, status.GitlinkHash)
	})

	t.Run("Detached", func(t *testing.T) {
		runGit(t, filepath.Join(parentDir, "libs", "core"),
			"checkout", "--detach", "HEAD",
		)
		status, err := parentWt.SubmoduleStatus(ctx, "libs/core")
		require.NoError(t, err)
		assert.Empty(t, status.Branch)
		assert.True(t, status.Detached)
	})
}

func TestSnapshotHead_attached(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	log := silogtest.New(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	wt, err := git.OpenWorktree(ctx, dir, git.OpenOptions{Log: log})
	require.NoError(t, err)

	snap, err := wt.SnapshotHead(ctx)
	require.NoError(t, err)
	assert.Equal(t, "main", snap.Branch)
	assert.False(t, snap.Detached)
	assert.NotEmpty(t, snap.Hash)
}

func TestSnapshotHead_detached(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	log := silogtest.New(t)

	dir := t.TempDir()
	initGitRepo(t, dir)
	runGit(t, dir, "checkout", "--detach", "HEAD")

	wt, err := git.OpenWorktree(ctx, dir, git.OpenOptions{Log: log})
	require.NoError(t, err)

	snap, err := wt.SnapshotHead(ctx)
	require.NoError(t, err)
	assert.Empty(t, snap.Branch)
	assert.True(t, snap.Detached)
	assert.NotEmpty(t, snap.Hash)
}

func TestRestoreHead_attached(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	log := silogtest.New(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	wt, err := git.OpenWorktree(ctx, dir, git.OpenOptions{Log: log})
	require.NoError(t, err)

	snap, err := wt.SnapshotHead(ctx)
	require.NoError(t, err)

	// Create another branch and switch to it.
	runGit(t, dir, "checkout", "-b", "feat")

	// Restore should bring us back to main.
	require.NoError(t, wt.RestoreHead(ctx, snap))

	cur, err := wt.CurrentBranch(ctx)
	require.NoError(t, err)
	assert.Equal(t, "main", cur)
}

func TestRestoreHead_detached(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	log := silogtest.New(t)

	dir := t.TempDir()
	initGitRepo(t, dir)
	// Add a second commit so we have something to detach to.
	runGit(t, dir, "commit", "--allow-empty", "-m", "second")

	wt, err := git.OpenWorktree(ctx, dir, git.OpenOptions{Log: log})
	require.NoError(t, err)

	// Detach at the previous commit.
	runGit(t, dir, "checkout", "--detach", "HEAD~1")
	snap, err := wt.SnapshotHead(ctx)
	require.NoError(t, err)
	require.True(t, snap.Detached)

	// Switch to a branch.
	runGit(t, dir, "checkout", "main")

	// Restoring should put us back at the detached commit.
	require.NoError(t, wt.RestoreHead(ctx, snap))

	_, err = wt.CurrentBranch(ctx)
	assert.ErrorIs(t, err, git.ErrDetachedHead)

	hash, err := wt.Head(ctx)
	require.NoError(t, err)
	assert.Equal(t, snap.Hash, hash)
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
