package git_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hexops/autogold/v2"
	"github.com/stretchr/testify/assert"
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
			Edit:    git.SkipCommitEdit(),
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
			Edit:    git.SkipCommitEdit(),
		}))

		assertCommitBodyEquals(t, autogold.Expect(`Body of the commit message.

Signed-off-by: Test Committer <committer@example.com>
Signed-off-by: Test Committer <signer@example.com>`))
	})
}

func TestWorktree_Commit_messageFile(t *testing.T) {
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

		git add file.txt

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

	messageFile := filepath.Join(fixture.Dir(), "message.txt")
	require.NoError(t, os.WriteFile(messageFile, []byte("Add test file\n\nFrom file.\n"), 0o644))

	require.NoError(t, worktree.Commit(ctx, git.CommitRequest{
		MessageFile: messageFile,
	}))

	commit, err := worktree.Repository().ReadCommit(ctx, "HEAD")
	require.NoError(t, err)
	require.Equal(t, "Add test file", commit.Subject)
	require.Equal(t, "From file.\n", commit.Body)
}

func TestWorktree_Commit_multipleMessages(t *testing.T) {
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

		git add file.txt

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
		Messages: []string{
			"Subject line",
			"Body paragraph one.",
			"Body paragraph two.",
		},
	}))

	commit, err := worktree.Repository().ReadCommit(ctx, "HEAD")
	require.NoError(t, err)
	require.Equal(t, "Subject line", commit.Subject)
	require.Contains(t, commit.Body,
		"Body paragraph one.\n\nBody paragraph two.")

	t.Run("SingleMessage", func(t *testing.T) {
		require.NoError(t, worktree.Commit(ctx, git.CommitRequest{
			AllowEmpty: true,
			Messages:   []string{"Only subject"},
		}))

		commit, err := worktree.Repository().ReadCommit(ctx, "HEAD")
		require.NoError(t, err)
		require.Equal(t, "Only subject", commit.Subject)
		require.Empty(t, commit.Body)
	})
}

func TestWorktree_Commit_customEditor(t *testing.T) {
	t.Setenv("GIT_EDITOR", "false")
	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		git init
		git add file.txt
		git commit -m 'Initial commit'

		-- file.txt --
		original
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{Log: silogtest.New(t)})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(fixture.Dir(), "file.txt"), []byte("updated\n"), 0o600))

	editor := filepath.Join(t.TempDir(), "editor ' $;.sh")
	require.NoError(t, os.WriteFile(editor, []byte(text.Dedent(`
		set -eu
		test "$#" = 2
		test "$1" = "$EXPECTED_ARGUMENT"
		git diff --cached -- file.txt | grep '^+updated$'
		grep '^Draft subject$' "$2"
		grep '^Signed-off-by:' "$2"
		trailer=$(grep '^Signed-off-by:' "$2")
		printf 'Edited subject\n\nEdited with prepared index\n\n%s\n' "$trailer" > "$2"
	`)), 0o600))

	argument := "spaces 'quotes' \"double\" $dollar `backticks` ; & \\backslash"
	require.NoError(t, wt.Commit(t.Context(), git.CommitRequest{
		Message:   "Draft subject",
		All:       true,
		Signoff:   true,
		Author:    &git.Signature{Name: "Test", Email: "test@example.com"},
		Committer: &git.Signature{Name: "Test", Email: "test@example.com"},
		Edit: git.EditCommitWith(git.CommitEditor{
			Args: []string{"sh", editor, argument},
			Env:  []string{"EXPECTED_ARGUMENT=" + argument},
		}),
	}))
	commit, err := wt.Repository().ReadCommit(t.Context(), "HEAD")
	require.NoError(t, err)
	assert.Equal(t, "Edited subject", commit.Subject)
	assert.Contains(t, commit.Body, "Signed-off-by: Test <test@example.com>")
	assert.Contains(t, commit.Body, "Edited with prepared index")
}

func TestWorktree_Commit_editorFailure(t *testing.T) {
	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		git init
		git commit --allow-empty -m 'Initial commit'
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)
	wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{Log: silogtest.New(t)})
	require.NoError(t, err)

	err = wt.Commit(t.Context(), git.CommitRequest{
		AllowEmpty: true,
		Author:     &git.Signature{Name: "Test", Email: "test@example.com"},
		Committer:  &git.Signature{Name: "Test", Email: "test@example.com"},
		Edit:       git.EditCommitWith(git.CommitEditor{Args: []string{"false"}}),
	})
	require.Error(t, err)
	commit, err := wt.Repository().ReadCommit(t.Context(), "HEAD")
	require.NoError(t, err)
	assert.Equal(t, "Initial commit", commit.Subject)
}
