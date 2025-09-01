package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/mockedit"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/text"
)

func TestRebase_deliberateInterrupt(t *testing.T) {
	t.Setenv("GIT_EDITOR", "mockedit")

	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		at '2024-05-21T20:30:40Z'

		git init
		git commit --allow-empty -m 'Initial commit'

		git add foo.txt
		git commit -m 'Add foo'

		git checkout -b feature

		git add bar.txt
		git commit -m 'Add bar'

		git add baz.txt
		git commit -m 'Add baz'

		git log --oneline HEAD
		cmp stdout $WORK/log.txt

		-- foo.txt --
		Contents of foo

		-- bar.txt --
		Contents of bar

		-- baz.txt --
		Contents of baz

		-- log.txt --
		d62d116 Add baz
		cc51432 Add bar
		44c553a Add foo
		2fd1f57 Initial commit
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	login(t, "foo")

	// Test cases with no InterruptFunc.
	// All must see RebseInterruptError.
	tests := []struct {
		name  string
		lines []string
	}{
		{
			name: "break",
			lines: []string{
				"pick cc51432 # Add bar",
				"break",
				"pick d62d116 # Add baz",
			},
		},
		{
			name: "edit",
			lines: []string{
				"pick cc51432 # Add bar",
				"edit d62d116 # Add baz",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			defer func() {
				assert.NoError(t, wt.RebaseAbort(ctx))
			}()
			mockedit.Expect(t).
				GiveLines(tt.lines...)

			err = wt.Rebase(ctx, git.RebaseRequest{
				Branch:      "feature",
				Upstream:    "main",
				Interactive: true,
			})
			require.Error(t, err)

			var rebaseErr *git.RebaseInterruptError
			require.ErrorAs(t, err, &rebaseErr)
			assert.Equal(t, &git.RebaseState{Branch: "feature"}, rebaseErr.State)
			assert.Equal(t, git.RebaseInterruptDeliberate, rebaseErr.Kind)
		})
	}
}

func TestRebase_unexpectedInterrupt(t *testing.T) {
	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		at '2024-05-21T20:30:40Z'

		git init
		git commit --allow-empty -m 'Initial commit'

		git add foo.txt
		git commit -m 'Add foo'

		git checkout -b feature
		git add bar.txt
		git commit -m 'Add bar'

		git checkout main
		mv conflicting-bar.txt bar.txt
		git add bar.txt
		git commit -m 'Conflicting bar'

		-- foo.txt --
		Contents of foo

		-- bar.txt --
		Contents of bar

		-- conflicting-bar.txt --
		Different contents of foo
	`)))
	require.NoError(t, err)

	wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	login(t, "user")

	t.Run("noInterruptFunc", func(t *testing.T) {
		ctx := t.Context()
		defer func() {
			assert.NoError(t, wt.RebaseAbort(ctx))
		}()

		err = wt.Rebase(ctx, git.RebaseRequest{
			Branch:   "feature",
			Upstream: "main",
		})
		require.Error(t, err)

		var rebaseErr *git.RebaseInterruptError
		require.ErrorAs(t, err, &rebaseErr)
		assert.Equal(t, &git.RebaseState{Branch: "feature"}, rebaseErr.State)
		assert.Equal(t, git.RebaseInterruptConflict, rebaseErr.Kind)
	})
}

func TestRebaseContinue_editor(t *testing.T) {
	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		at '2024-05-21T20:30:40Z'

		git init
		git commit --allow-empty -m 'Initial commit'

		git add foo.txt
		git commit -m 'Add foo'

		git checkout -b feature
		git add bar.txt
		git commit -m 'Add bar'

		git checkout main
		mv conflicting-bar.txt bar.txt
		git add bar.txt
		git commit -m 'Conflicting bar'

		-- foo.txt --
		Contents of foo

		-- bar.txt --
		Contents of bar

		-- conflicting-bar.txt --
		Different contents of foo
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)
	login(t, "foo")

	mockedit.Expect(t).
		GiveLines(
			"pick cc51432 Add bar",
			"break",
			"pick 7dd9ddf Add baz",
		)

	err = wt.Rebase(t.Context(), git.RebaseRequest{
		Branch:   "feature",
		Upstream: "main",
	})
	require.Error(t, err)

	var rebaseErr *git.RebaseInterruptError
	require.ErrorAs(t, err, &rebaseErr)
	assert.Equal(t, git.RebaseInterruptConflict, rebaseErr.Kind,
		"rebase should be interrupted by a conflict")

	// Fix the conflict.
	require.NoError(t, os.WriteFile(
		filepath.Join(fixture.Dir(), "bar.txt"),
		[]byte("Merged contents of bar"), 0o644))

	addCmd := exec.Command("git", "add", "bar.txt")
	addCmd.Dir = fixture.Dir()
	require.NoError(t, addCmd.Run(), "git add bar.txt should succeed")

	mockedit.ExpectNone(t)
	err = wt.RebaseContinue(t.Context(), &git.RebaseContinueOptions{
		Editor: "true", // no edit
	})
	require.NoError(t, err, "rebase continue should use custom editor")

	// Verify resolved file.
	bs, err := os.ReadFile(filepath.Join(fixture.Dir(), "bar.txt"))
	require.NoError(t, err, "reading bar.txt should succeed")
	assert.Equal(t, "Merged contents of bar", string(bs), "bar.txt should contain merged contents")

	// Verify commit message of resolved commit.
	commits, err := sliceutil.CollectErr(
		wt.Repository().ListCommitsDetails(t.Context(),
			git.CommitRangeFrom("feature").ExcludeFrom("main")))
	require.NoError(t, err)

	if assert.Len(t, commits, 1, "should have one commit in feature branch") {
		assert.Equal(t, "Add bar", commits[0].Subject, "original commit message should be preserved")
	}
}

func TestRebase_autostashConflict(t *testing.T) {
	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		at '2025-06-21T09:27:19Z'

		git init
		git add base.txt
		git commit -m 'Initial commit'

		git checkout -b feature
		git add feature.txt
		git commit -m 'Add feature'

		git checkout main
		git add modified-base.txt
		mv modified-base.txt base.txt
		git add base.txt
		git commit -m 'Modify base'

		git checkout feature

		-- base.txt --
		Base content

		-- feature.txt --
		Feature content

		-- modified-base.txt --
		Modified base content that will conflict
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)
	login(t, "foo")

	ctx := t.Context()

	// Make changes to feature branch's base.txt
	// that will conflict with main's base.txt.
	conflictingContent := "Different content that conflicts"
	require.NoError(t, os.WriteFile(
		filepath.Join(fixture.Dir(), "base.txt"),
		[]byte(conflictingContent), 0o644))

	err = wt.Rebase(ctx, git.RebaseRequest{
		Branch:    "feature",
		Upstream:  "main",
		Autostash: true,
	})

	require.Error(t, err)

	assert.NotErrorAs(t, err, new(*git.RebaseInterruptError),
		"rebase should not return RebaseInterruptError for autostash conflict")
	assert.ErrorContains(t, err, "dirty changes could not be re-applied")

	unmergedFiles, err := sliceutil.CollectErr(
		wt.ListFilesPaths(ctx, &git.ListFilesOptions{Unmerged: true}))
	require.NoError(t, err)
	assert.Equal(t, []string{"base.txt"}, unmergedFiles)
}

func TestRebase_autostashSuccess(t *testing.T) {
	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		at '2025-06-21T09:27:19Z'

		git init
		git add base.txt
		git commit -m 'Initial commit'

		git checkout -b feature
		git add feature.txt
		git commit -m 'Add feature'

		git checkout main
		git add other.txt
		git commit -m 'Add other file'

		git checkout feature

		-- base.txt --
		Base content

		-- feature.txt --
		Feature content

		-- other.txt --
		Other content
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)
	login(t, "foo")

	ctx := t.Context()

	// Create dirty changes that won't conflict
	nonConflictingContent := "Modified base content"
	require.NoError(t, os.WriteFile(
		filepath.Join(fixture.Dir(), "base.txt"),
		[]byte(nonConflictingContent), 0o644))

	// Rebase with autostash should succeed
	err = wt.Rebase(ctx, git.RebaseRequest{
		Branch:    "feature",
		Upstream:  "main",
		Autostash: true,
	})
	require.NoError(t, err)

	// Verify dirty changes were re-applied
	content, err := os.ReadFile(filepath.Join(fixture.Dir(), "base.txt"))
	require.NoError(t, err)
	assert.Equal(t, nonConflictingContent, string(content))

	// Verify no unmerged files
	unmergedFiles, err := sliceutil.CollectErr(
		wt.ListFilesPaths(ctx, &git.ListFilesOptions{Unmerged: true}))
	require.NoError(t, err)
	assert.Empty(t, unmergedFiles)
}

func login(t testing.TB, username string) (home string) {
	require.NotEmpty(t, username, "username must not be empty")
	require.NotContains(t, username, " ", "username must not contain spaces")

	home = filepath.Join(t.TempDir(), username)
	require.NoError(t, os.MkdirAll(home, 0o700))

	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_AUTHOR_NAME", username)
	t.Setenv("GIT_AUTHOR_EMAIL", username+"@example.com")
	t.Setenv("GIT_COMMITTER_NAME", username)
	t.Setenv("GIT_COMMITTER_EMAIL", username+"@example.com")
	return home
}
