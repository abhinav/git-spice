package git_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/text"
)

func TestWorktree_DiffWork(t *testing.T) {
	t.Parallel()

	t.Run("UnstagedChanges", func(t *testing.T) {
		t.Parallel()

		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			as 'Test <test@example.com>'
			at '2025-06-21T10:00:00Z'

			git init
			git add committed.txt
			git add to-be-modified.txt
			git add to-be-deleted.txt
			git commit -m 'Initial commit'

			# Modify a file and stage it
			cp $WORK/extra/staged.txt to-be-modified.txt
			git add to-be-modified.txt

			# Modify a file but don't stage it
			cp $WORK/extra/unstaged.txt committed.txt

			# Delete a file and stage the deletion
			git rm to-be-deleted.txt

			# Add a new file and stage it
			git add new-staged.txt

			-- committed.txt --
			original committed content
			-- to-be-modified.txt --
			original content
			-- to-be-deleted.txt --
			will be deleted
			-- new-staged.txt --
			new file content
			-- extra/staged.txt --
			modified and staged
			-- extra/unstaged.txt --
			modified but unstaged
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		files, err := sliceutil.CollectErr(wt.DiffWork(t.Context()))
		require.NoError(t, err)

		expected := []git.FileStatus{
			{Status: "M", Path: "committed.txt"},
		}
		assert.ElementsMatch(t, expected, files)
	})

	t.Run("NoChanges", func(t *testing.T) {
		t.Parallel()

		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			as 'Test <test@example.com>'
			at '2025-06-21T10:00:00Z'

			git init
			git add file1.txt
			git commit -m 'Initial commit'

			-- file1.txt --
			content
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		files, err := sliceutil.CollectErr(wt.DiffWork(t.Context()))
		require.NoError(t, err)
		assert.Empty(t, files)
	})

	t.Run("DeletedFile", func(t *testing.T) {
		t.Parallel()

		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			as 'Test <test@example.com>'
			at '2025-06-21T10:00:00Z'

			git init
			git add file1.txt
			git commit -m 'Initial commit'

			rm file1.txt

			-- file1.txt --
			content
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		files, err := sliceutil.CollectErr(wt.DiffWork(t.Context()))
		require.NoError(t, err)

		expected := []git.FileStatus{
			{Status: "D", Path: "file1.txt"},
		}
		assert.ElementsMatch(t, expected, files)
	})
}

func TestWorktree_DiffIndex(t *testing.T) {
	t.Parallel()

	t.Run("StagedChanges", func(t *testing.T) {
		t.Parallel()

		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			as 'Test <test@example.com>'
			at '2025-06-21T10:00:00Z'

			git init
			git add file1.txt
			git add file2.txt
			git add file3.txt
			git commit -m 'Initial commit'

			git add file4.txt
			git commit -m 'Add file4'

			# Stage various changes
			cp $WORK/extra/modified.txt file2.txt
			git add file2.txt

			git rm file3.txt

			git add new-file.txt

			-- file1.txt --
			unchanged
			-- file2.txt --
			original content
			-- file3.txt --
			to be deleted
			-- file4.txt --
			added in second commit
			-- new-file.txt --
			newly added
			-- extra/modified.txt --
			modified content
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		t.Run("CompareWithHEAD", func(t *testing.T) {
			files, err := wt.DiffIndex(t.Context(), "HEAD")
			require.NoError(t, err)

			expected := []git.FileStatus{
				{Status: "M", Path: "file2.txt"},
				{Status: "D", Path: "file3.txt"},
				{Status: "A", Path: "new-file.txt"},
			}
			assert.ElementsMatch(t, expected, files)
		})

		t.Run("CompareWithHEAD~1", func(t *testing.T) {
			files, err := wt.DiffIndex(t.Context(), "HEAD~1")
			require.NoError(t, err)

			expected := []git.FileStatus{
				{Status: "M", Path: "file2.txt"},
				{Status: "D", Path: "file3.txt"},
				{Status: "A", Path: "file4.txt"},
				{Status: "A", Path: "new-file.txt"},
			}
			assert.ElementsMatch(t, expected, files)
		})
	})

	t.Run("NoChanges", func(t *testing.T) {
		t.Parallel()

		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			as 'Test <test@example.com>'
			at '2025-06-21T10:00:00Z'

			git init
			git add file1.txt
			git commit -m 'Initial commit'

			-- file1.txt --
			content
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		files, err := wt.DiffIndex(t.Context(), "HEAD")
		require.NoError(t, err)
		assert.Empty(t, files)
	})
}

func TestRepository_DiffTree(t *testing.T) {
	t.Parallel()

	t.Run("CompareCommitsAndBranches", func(t *testing.T) {
		t.Parallel()

		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			as 'Test <test@example.com>'
			at '2025-06-21T10:00:00Z'

			git init
			git add base.txt
			git add will-modify.txt
			git add will-delete.txt
			git commit -m 'Initial commit'

			git checkout -b feature
			cp $WORK/extra/modified.txt will-modify.txt
			git add will-modify.txt
			git rm will-delete.txt
			git add new-file.txt
			git commit -m 'Feature changes'

			git checkout main
			git add another-file.txt
			git commit -m 'Main changes'

			-- base.txt --
			base content
			-- will-modify.txt --
			original
			-- will-delete.txt --
			will be deleted
			-- new-file.txt --
			new in feature
			-- another-file.txt --
			new in main
			-- extra/modified.txt --
			modified
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		repo, err := git.Open(t.Context(), fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		t.Run("CompareTwoCommits", func(t *testing.T) {
			files, err := sliceutil.CollectErr(repo.DiffTree(t.Context(), "HEAD~1", "HEAD"))
			require.NoError(t, err)

			expected := []git.FileStatus{
				{Status: "A", Path: "another-file.txt"},
			}
			assert.ElementsMatch(t, expected, files)
		})

		t.Run("CompareBranches", func(t *testing.T) {
			files, err := sliceutil.CollectErr(repo.DiffTree(t.Context(), "main", "feature"))
			require.NoError(t, err)

			expected := []git.FileStatus{
				{Status: "M", Path: "will-modify.txt"},
				{Status: "D", Path: "will-delete.txt"},
				{Status: "A", Path: "new-file.txt"},
				{Status: "D", Path: "another-file.txt"},
			}
			assert.ElementsMatch(t, expected, files)
		})

		t.Run("CompareIdenticalTrees", func(t *testing.T) {
			files, err := sliceutil.CollectErr(repo.DiffTree(t.Context(), "HEAD", "HEAD"))
			require.NoError(t, err)
			assert.Empty(t, files)
		})
	})

	t.Run("MultipleChanges", func(t *testing.T) {
		t.Parallel()

		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			as 'Test <test@example.com>'
			at '2025-06-21T10:00:00Z'

			git init
			git add a.txt
			git add b.txt
			git add c.txt
			git add d.txt
			git commit -m 'Initial commit'

			cp $WORK/extra/modified.txt a.txt
			git add a.txt
			git rm b.txt
			git add e.txt
			git commit -m 'Multiple changes'

			-- a.txt --
			original a
			-- b.txt --
			original b
			-- c.txt --
			unchanged c
			-- d.txt --
			unchanged d
			-- e.txt --
			new e
			-- extra/modified.txt --
			modified a
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		repo, err := git.Open(t.Context(), fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		files, err := sliceutil.CollectErr(repo.DiffTree(t.Context(), "HEAD~1", "HEAD"))
		require.NoError(t, err)

		expected := []git.FileStatus{
			{Status: "M", Path: "a.txt"},
			{Status: "D", Path: "b.txt"},
			{Status: "A", Path: "e.txt"},
		}
		assert.ElementsMatch(t, expected, files)
	})
}
