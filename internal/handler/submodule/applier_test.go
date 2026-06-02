package submodule_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/submodule"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

func TestApplier_ApplyAssociations_happyPath(t *testing.T) {
	t.Parallel()

	fix := newApplierFixture(t)
	fix.addSubBranch(t, "libs/core", "feat-core")

	// Record an association for the parent branch.
	fix.recordAssociation(t, "feature", map[string]string{
		"libs/core": "feat-core",
	})

	require.NoError(t, fix.applier.ApplyAssociations(t.Context(), "feature"))

	// Sub should be on feat-core now.
	cur, err := fix.parentWT.SubmoduleCurrentBranch(t.Context(), "libs/core")
	require.NoError(t, err)
	assert.Equal(t, "feat-core", cur)
}

func TestApplier_ApplyAssociations_alreadyOnRecorded(t *testing.T) {
	t.Parallel()

	fix := newApplierFixture(t)
	fix.addSubBranch(t, "libs/core", "feat-core")

	// Sub is already on feat-core; recording matches.
	runGit(t, filepath.Join(fix.parentDir, "libs", "core"),
		"checkout", "feat-core")
	fix.recordAssociation(t, "feature", map[string]string{
		"libs/core": "feat-core",
	})

	// Apply should be a no-op.
	require.NoError(t, fix.applier.ApplyAssociations(t.Context(), "feature"))

	cur, err := fix.parentWT.SubmoduleCurrentBranch(t.Context(), "libs/core")
	require.NoError(t, err)
	assert.Equal(t, "feat-core", cur)
}

func TestApplier_ApplyAssociations_noRecordIsNoop(t *testing.T) {
	t.Parallel()

	fix := newApplierFixture(t)

	// No association recorded for "feature" — should be a no-op.
	require.NoError(t, fix.applier.ApplyAssociations(t.Context(), "feature"))
}

func TestApplier_ApplyAssociations_excluded(t *testing.T) {
	t.Parallel()

	fix := newApplierFixture(t)
	fix.addSubBranch(t, "libs/core", "feat-core")
	fix.recordAssociation(t, "feature", map[string]string{
		"libs/core": "feat-core",
	})

	// Exclude libs/core; the apply should skip it.
	fix.applier.Exclude = []string{"libs/core"}

	require.NoError(t, fix.applier.ApplyAssociations(t.Context(), "feature"))

	// Sub should still be on main.
	cur, err := fix.parentWT.SubmoduleCurrentBranch(t.Context(), "libs/core")
	require.NoError(t, err)
	assert.Equal(t, "main", cur)
}

func TestApplier_ApplyAssociations_rollbackOnFailure(t *testing.T) {
	t.Parallel()

	fix := newApplierFixture(t)
	fix.addSubBranch(t, "libs/a", "feat-a")
	fix.addSubBranch(t, "libs/b", "feat-b")

	// Record associations for both; libs/b's branch does not exist
	// so its checkout will fail.
	fix.recordAssociation(t, "feature", map[string]string{
		"libs/a": "feat-a",
		"libs/b": "does-not-exist",
	})

	err := fix.applier.ApplyAssociations(t.Context(), "feature")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "libs/b")

	// Both subs should be on main: libs/a was switched to feat-a
	// and then rolled back; libs/b never switched.
	curA, err := fix.parentWT.SubmoduleCurrentBranch(t.Context(), "libs/a")
	require.NoError(t, err)
	assert.Equal(t, "main", curA, "libs/a should be rolled back")

	curB, err := fix.parentWT.SubmoduleCurrentBranch(t.Context(), "libs/b")
	require.NoError(t, err)
	assert.Equal(t, "main", curB)
}

// applierFixture provides a real parent+submodule(s) on disk
// for end-to-end Applier tests.
type applierFixture struct {
	parentDir string
	parentWT  *git.Worktree
	store     *state.Store
	applier   *submodule.Applier
}

func newApplierFixture(t *testing.T) *applierFixture {
	t.Helper()

	parentDir := t.TempDir()
	initGitRepo(t, parentDir)

	log := silogtest.New(t)

	parentWT, err := git.OpenWorktree(t.Context(), parentDir, git.OpenOptions{
		Log: log,
	})
	require.NoError(t, err)

	// Set up a gs store on the parent.
	db := storage.NewDB(storage.NewGitBackend(storage.GitConfig{
		Repo:        parentWT.Repository(),
		Ref:         "refs/spice/data",
		AuthorName:  "test",
		AuthorEmail: "test@example.com",
		Log:         log,
	}))
	store, err := state.InitStore(t.Context(), state.InitStoreRequest{
		DB:    db,
		Trunk: "main",
	})
	require.NoError(t, err)

	return &applierFixture{
		parentDir: parentDir,
		parentWT:  parentWT,
		store:     store,
		applier: &submodule.Applier{
			Log:      log,
			Worktree: parentWT,
			Store:    store,
		},
	}
}

// addSubBranch adds a fresh submodule at path with one extra
// branch (in addition to the default main).
func (f *applierFixture) addSubBranch(t *testing.T, path, branch string) {
	t.Helper()
	subDir := t.TempDir()
	initGitRepo(t, subDir)
	runGit(t, subDir, "checkout", "-b", branch)
	runGit(t, subDir, "commit", "--allow-empty", "-m", "for "+branch)
	runGit(t, subDir, "checkout", "main")

	addSubmodule(t, f.parentDir, subDir, path)
	runGit(t, f.parentDir, "commit", "-m", "add "+path)

	// Fetch the new branch into the embedded submodule clone so it can be
	// checked out by name later.
	runGit(t, filepath.Join(f.parentDir, path), "fetch", "origin", branch+":"+branch)
}

func (f *applierFixture) recordAssociation(
	t *testing.T, branch string, subs map[string]string,
) {
	t.Helper()
	tx := f.store.BeginBranchTx()
	require.NoError(t, tx.Upsert(t.Context(), state.UpsertRequest{
		Name:       branch,
		Base:       "main",
		BaseHash:   "0000000000000000000000000000000000000000",
		Submodules: subs,
	}))
	require.NoError(t, tx.Commit(t.Context(), "record "+branch))
}

// Helpers below mirror internal/git/submodule_test.go's helpers.

func initGitRepo(t *testing.T, dir string) {
	t.Helper()

	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "commit", "--allow-empty", "-m", "init")
}

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

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

// silence unused-import warnings in this file.
var _ = context.Background
