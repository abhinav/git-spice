package spice

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/logutil"
	"go.abhg.dev/gs/internal/spice/state"
)

func TestService_BranchOnto_moveOntoUpstackOfOldBase(t *testing.T) {
	t.Parallel()

	author := &git.Signature{
		Name:  "Test User",
		Email: "test@example.com",
		Time:  time.Date(2025, 2, 21, 10, 48, 32, 5, time.UTC),
	}

	dir := t.TempDir()
	repo, err := git.Init(t.Context(), dir, git.InitOptions{
		Log:    logutil.TestLogger(t),
		Branch: "main",
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), nil, 0o644))

	ctx := t.Context()
	{
		cmd := exec.CommandContext(ctx, "git", "add", "file.txt")
		cmd.Dir = dir
		require.NoError(t, cmd.Run())
	}

	require.NoError(t, err,
		repo.Commit(ctx, git.CommitRequest{
			Message:   "init",
			Author:    author,
			Committer: author,
		}))

	initCommit, err := repo.Head(ctx)
	require.NoError(t, err)

	initTree, err := repo.PeelToTree(ctx, initCommit.String())
	require.NoError(t, err)

	// We'll create two branches adding conflicting contents
	// to the same file.
	branch1, branch2 := "foo", "bar"

	// 1. Create the blobs
	blob1, err := repo.WriteObject(ctx, git.BlobType, strings.NewReader(branch1))
	require.NoError(t, err)
	blob2, err := repo.WriteObject(ctx, git.BlobType, strings.NewReader(branch2))
	require.NoError(t, err)

	// 2. Build the trees with those blobs
	tree1, err := repo.UpdateTree(ctx, git.UpdateTreeRequest{
		Tree:   initTree,
		Writes: []git.BlobInfo{{Hash: blob1, Path: "file.txt"}},
	})
	require.NoError(t, err)
	tree2, err := repo.UpdateTree(ctx, git.UpdateTreeRequest{
		Tree:   initTree,
		Writes: []git.BlobInfo{{Hash: blob2, Path: "file.txt"}},
	})
	require.NoError(t, err)

	// 3. Build commits from the trees
	commit1, err := repo.CommitTree(ctx, git.CommitTreeRequest{
		Tree:      tree1,
		Message:   branch1,
		Parents:   []git.Hash{initCommit},
		Author:    author,
		Committer: author,
	})
	require.NoError(t, err)
	commit2, err := repo.CommitTree(ctx, git.CommitTreeRequest{
		Tree:      tree2,
		Message:   branch2,
		Parents:   []git.Hash{initCommit},
		Author:    author,
		Committer: author,
	})
	require.NoError(t, err)

	// 4. Create branch refs from commits.
	require.NoError(t, repo.CreateBranch(ctx, git.CreateBranchRequest{
		Name: branch1,
		Head: commit1.String(),
	}))
	require.NoError(t, repo.CreateBranch(ctx, git.CreateBranchRequest{
		Name: branch2,
		Head: commit2.String(),
	}))

	// Track the two branches with "main" as base.
	store := NewMemoryStore(t)
	func() {
		tx := store.BeginBranchTx()

		require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
			Name:     branch1,
			Base:     "main",
			BaseHash: initCommit,
		}))

		require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
			Name:     branch2,
			Base:     "main",
			BaseHash: initCommit,
		}))

		require.NoError(t, tx.Commit(ctx, "initialize branch state"))
	}()

	svc := NewTestService(repo, store, nil, logutil.TestLogger(t))

	// Move branch1 onto branch2.
	// This will encounter a conflict.
	err = svc.BranchOnto(t.Context(), &BranchOntoRequest{
		Branch: branch1,
		Onto:   branch2,
	})
	require.Error(t, err)
	var rebaseErr *git.RebaseInterruptError
	require.ErrorAs(t, err, &rebaseErr)

	// Resolve the rebase conflict.
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "file.txt"), []byte("resolved"), 0o644))
	{
		cmd := exec.CommandContext(ctx, "git", "add", "file.txt")
		cmd.Dir = dir
		require.NoError(t, cmd.Run())
	}
	require.NoError(t, repo.WithEditor("true").RebaseContinue(ctx))

	// Sanity check:
	// base for branch1 is still main because the operation failed.
	{
		info1, err := svc.LookupBranch(ctx, branch1)
		require.NoError(t, err)
		assert.Equal(t, "main", info1.Base)
	}

	// Move branch onto branch2 again. This should succeed.
	require.NoError(t, svc.BranchOnto(t.Context(), &BranchOntoRequest{
		Branch: branch1,
		Onto:   branch2,
	}))

	{
		info1, err := svc.LookupBranch(ctx, branch1)
		require.NoError(t, err)
		assert.Equal(t, branch2, info1.Base)
	}
}
