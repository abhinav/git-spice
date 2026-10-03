package forgetest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/xec"
)

// RepositoryBuilder provisions commits and branches in a disposable clone of
// a forge integration-test repository.
//
// RepositoryBuilder is available only while recording fixtures.
// The test owns any remote state it creates:
// callers must register cleanup for every branch they push.
// Local state lives in the test's temporary directory and is removed by the
// testing package.
type RepositoryBuilder struct {
	repo *git.Repository
	work *git.Worktree
	root string
	t    *testing.T
}

// NewRepositoryBuilder clones remoteURL and disables commit signing in the
// clone so tests can commit without relying on user configuration.
// It fails the test if fixture recording is disabled or setup fails.
func NewRepositoryBuilder(t *testing.T, remoteURL string) *RepositoryBuilder {
	require.True(t, Update(), "RepositoryBuilder only available in update mode")

	repoDir := t.TempDir()
	output := t.Output()
	cmd := xec.Command(t.Context(), silogtest.New(t), "git", "clone", remoteURL, repoDir).
		WithStdout(output).
		WithStderr(output)
	require.NoError(t, cmd.Run(), "failed to clone repository")

	require.NoError(t, xec.Command(
		t.Context(),
		silogtest.New(t),
		"git", "config", "commit.gpgsign", "false",
	).
		WithDir(repoDir).
		WithStdout(output).
		WithStderr(output).
		Run(), "disable commit signing")

	ctx := t.Context()
	work, err := git.OpenWorktree(ctx, repoDir, git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err, "failed to open git worktree")

	return &RepositoryBuilder{
		repo: work.Repository(),
		work: work,
		root: repoDir,
		t:    t,
	}
}

func (r *RepositoryBuilder) ctx() context.Context {
	ctx := r.t.Context()
	// If the context was canceled, ignore its cancellation.
	if errors.Is(ctx.Err(), context.Canceled) {
		ctx = context.WithoutCancel(ctx)
	}
	return ctx
}

// WriteFile replaces the repository-relative path with the given lines.
// It creates parent directories and terminates non-empty content with a newline.
func (r *RepositoryBuilder) WriteFile(path string, lines ...string) {
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	require.NoError(r.t, os.MkdirAll(
		filepath.Dir(filepath.Join(r.root, path)),
		0o755,
	), "could not create directories for file: %s", path)
	require.NoError(r.t, os.WriteFile(
		filepath.Join(r.root, path),
		[]byte(content),
		0o644,
	), "could not write file: %s", path)
}

// AddAllAndCommit stages the entire worktree and commits it with the shared
// integration-test identity.
// It returns the new commit's hash.
func (r *RepositoryBuilder) AddAllAndCommit(message string) git.Hash {
	output := r.t.Output()
	cmd := xec.Command(r.t.Context(), silogtest.New(r.t), "git", "add", ".").
		WithDir(r.root).
		WithStdout(output).
		WithStderr(output)
	require.NoError(r.t, cmd.Run(), "git add failed")

	ctx := r.ctx()
	sig := git.Signature{
		Name:  "gs-test[bot]",
		Email: "bot@example.com",
	}
	require.NoError(r.t, r.work.Commit(ctx, git.CommitRequest{
		Message:   message,
		Author:    &sig,
		Committer: &sig,
	}), "could not commit changes")

	hash, err := r.repo.PeelToCommit(ctx, "HEAD")
	require.NoError(r.t, err, "could not get commit hash")
	return hash
}

// CreateBranch creates name at the current HEAD without checking it out.
func (r *RepositoryBuilder) CreateBranch(name string) {
	ctx := r.ctx()
	require.NoError(r.t, r.repo.CreateBranch(ctx, git.CreateBranchRequest{
		Name: name,
	}), "could not create branch: %s", name)
}

// CheckoutBranch checks out an existing local branch.
func (r *RepositoryBuilder) CheckoutBranch(name string) {
	ctx := r.ctx()
	require.NoError(r.t, r.work.CheckoutBranch(ctx, name),
		"could not checkout branch: %s", name)
}

// Push sends refspec to the clone's origin remote.
func (r *RepositoryBuilder) Push(refspec string) {
	r.PushTo("origin", refspec)
}

// AddRemote adds name with remoteURL to the clone.
func (r *RepositoryBuilder) AddRemote(name, remoteURL string) {
	output := r.t.Output()
	cmd := xec.Command(
		r.ctx(),
		silogtest.New(r.t),
		"git", "remote", "add", name, remoteURL,
	).
		WithDir(r.root).
		WithStdout(output).
		WithStderr(output)
	require.NoError(r.t, cmd.Run(), "could not add remote: %s", name)
}

// PushTo sends refspec to the named remote.
func (r *RepositoryBuilder) PushTo(remote, refspec string) {
	ctx := r.ctx()
	require.NoError(r.t, r.work.Push(ctx, git.PushOptions{
		Remote:  remote,
		Refspec: git.Refspec(refspec),
	}), "error pushing refspec %s to %s", refspec, remote)
}

// DeleteRemoteBranch attempts to delete name from the origin remote.
// Cleanup failures are logged instead of failing the test.
func (r *RepositoryBuilder) DeleteRemoteBranch(name string) {
	r.DeleteRemoteBranchFrom("origin", name)
}

// DeleteRemoteBranchFrom attempts to delete name from the named remote.
// Cleanup failures are logged instead of failing the test.
func (r *RepositoryBuilder) DeleteRemoteBranchFrom(remote, name string) {
	ctx := r.ctx()
	r.t.Logf("Deleting remote branch: %s/%s", remote, name)
	if err := r.work.Push(ctx, git.PushOptions{
		Remote:  remote,
		Refspec: git.Refspec(":" + name),
	}); err != nil {
		r.t.Logf("Warning: failed to delete remote branch %s/%s: %v", remote, name, err)
	}
}

// Repository returns the repository for the builder's temporary clone.
// The value remains valid until the test completes.
func (r *RepositoryBuilder) Repository() *git.Repository {
	return r.repo
}

// Worktree returns the worktree for the builder's temporary clone.
// The value remains valid until the test completes.
func (r *RepositoryBuilder) Worktree() *git.Worktree {
	return r.work
}

// randomString generates a random alphanumeric string of length n.
