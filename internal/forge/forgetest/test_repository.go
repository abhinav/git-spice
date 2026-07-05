package forgetest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/xec"
)

type testRepository struct {
	repo *git.Repository
	work *git.Worktree
	root string
	t    *testing.T
}

func newTestRepository(t *testing.T, remoteURL string) *testRepository {
	require.True(t, Update(), "testRepository only available in update mode")

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

	return &testRepository{
		repo: work.Repository(),
		work: work,
		root: repoDir,
		t:    t,
	}
}

func (r *testRepository) ctx() context.Context {
	ctx := r.t.Context()
	// If the context was canceled, ignore its cancellation.
	if errors.Is(ctx.Err(), context.Canceled) {
		ctx = context.WithoutCancel(ctx)
	}
	return ctx
}

// WriteFile writes a file to the repository with the given lines.
func (r *testRepository) WriteFile(path string, lines ...string) {
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

// AddAllAndCommit stages all changes and creates a commit.
func (r *testRepository) AddAllAndCommit(message string) git.Hash {
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

// CreateBranch creates a new branch.
func (r *testRepository) CreateBranch(name string) {
	ctx := r.ctx()
	require.NoError(r.t, r.repo.CreateBranch(ctx, git.CreateBranchRequest{
		Name: name,
	}), "could not create branch: %s", name)
}

// CheckoutBranch checks out an existing branch.
func (r *testRepository) CheckoutBranch(name string) {
	ctx := r.ctx()
	require.NoError(r.t, r.work.CheckoutBranch(ctx, name),
		"could not checkout branch: %s", name)
}

// Push pushes the given refspec to origin.
func (r *testRepository) Push(refspec string) {
	r.PushTo("origin", refspec)
}

// AddRemote adds a remote to the test repository.
func (r *testRepository) AddRemote(name, remoteURL string) {
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

// PushTo pushes the given refspec to a remote.
func (r *testRepository) PushTo(remote, refspec string) {
	ctx := r.ctx()
	require.NoError(r.t, r.work.Push(ctx, git.PushOptions{
		Remote:  remote,
		Refspec: git.Refspec(refspec),
	}), "error pushing refspec %s to %s", refspec, remote)
}

// DeleteRemoteBranch deletes a remote branch.
func (r *testRepository) DeleteRemoteBranch(name string) {
	r.DeleteRemoteBranchFrom("origin", name)
}

// DeleteRemoteBranchFrom deletes a branch from a remote.
func (r *testRepository) DeleteRemoteBranchFrom(remote, name string) {
	ctx := r.ctx()
	r.t.Logf("Deleting remote branch: %s/%s", remote, name)
	assert.NoError(r.t, r.work.Push(ctx, git.PushOptions{
		Remote:  remote,
		Refspec: git.Refspec(":" + name),
	}), "error deleting branch")
}

// Repository returns the underlying git.Repository.
func (r *testRepository) Repository() *git.Repository {
	return r.repo
}

// Worktree returns the underlying git.Worktree.
func (r *testRepository) Worktree() *git.Worktree {
	return r.work
}

// randomString generates a random alphanumeric string of length n.
