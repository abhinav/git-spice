package git_test

import (
	"strings"
	"testing"

	"github.com/hexops/autogold/v2"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/text"
)

func TestWorktree_Commit_signoff(t *testing.T) {
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("USER", "testuser")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_COMMITTER_NAME", "Test Committer")
	t.Setenv("GIT_COMMITTER_EMAIL", "committer@example.com")
	t.Setenv("GIT_AUTHOR_NAME", "Test Author")
	t.Setenv("GIT_AUTHOR_EMAIL", "author@example.com")

	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		at '2025-08-30T21:28:29Z'
		as 'Test Owner <test@example.com>'

		git init
		git commit --allow-empty -m 'Initial commit'

		git add file.txt  # used to commit below

		-- file.txt --
		test content
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	ctx := t.Context()
	worktree, err := git.OpenWorktree(ctx, fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	require.NoError(t, worktree.Commit(ctx, git.CommitRequest{
		Message: "Add test file\n\nBody of the commit message.",
		Signoff: true,
	}))

	repo := worktree.Repository()

	assertCommitBodyEquals := func(t *testing.T, value autogold.Value) {
		commit, err := repo.ReadCommit(ctx, "HEAD")
		require.NoError(t, err)
		value.Equal(t, strings.TrimSpace(commit.Body))
	}

	assertCommitBodyEquals(t, autogold.Expect(`Body of the commit message.

Signed-off-by: Test Committer <committer@example.com>`))

	t.Run("AmendAlreadySignedOff", func(t *testing.T) {
		require.NoError(t, worktree.Commit(ctx, git.CommitRequest{
			Amend:   true,
			Signoff: true,
			NoEdit:  true,
		}))

		assertCommitBodyEquals(t, autogold.Expect(`Body of the commit message.

Signed-off-by: Test Committer <committer@example.com>`))
	})

	t.Run("AmendAlreadySignedOffNewAuthor", func(t *testing.T) {
		t.Setenv("GIT_AUTHOR_NAME", "Test Signer")
		t.Setenv("GIT_COMMITTER_EMAIL", "signer@example.com")
		require.NoError(t, worktree.Commit(ctx, git.CommitRequest{
			Amend:   true,
			Signoff: true,
			NoEdit:  true,
		}))

		assertCommitBodyEquals(t, autogold.Expect(`Body of the commit message.

Signed-off-by: Test Committer <committer@example.com>
Signed-off-by: Test Committer <signer@example.com>`))
	})
}
