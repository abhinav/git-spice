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

func TestListFilesPaths(t *testing.T) {
	t.Parallel()

	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		at '2025-06-21T09:27:19Z'

		git init
		git add file1.txt
		git commit -m 'Initial commit'

		git add file2.txt
		git commit -m 'Add file2'

		-- file1.txt --
		Contents of file1

		-- file2.txt --
		Contents of file2
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	paths, err := sliceutil.CollectErr(wt.ListFilesPaths(t.Context(), nil))
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"file1.txt", "file2.txt"}, paths)
}

func TestListFilesPaths_unmerged(t *testing.T) {
	t.Parallel()

	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		at '2025-06-21T09:27:19Z'

		git init
		git add base.txt
		git commit -m 'Initial commit'

		git checkout -b feature
		git add conflict.txt
		git commit -m 'Add conflict file'

		git checkout main
		mv different-conflict.txt conflict.txt
		git add conflict.txt
		git commit -m 'Add different conflict file'

		! git merge feature

		-- base.txt --
		Base file

		-- conflict.txt --
		Feature version

		-- different-conflict.txt --
		Main version
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	t.Run("ListAll", func(t *testing.T) {
		paths, err := sliceutil.CollectErr(
			wt.ListFilesPaths(t.Context(), nil))
		require.NoError(t, err)

		assert.Contains(t, paths, "base.txt")
		assert.Contains(t, paths, "conflict.txt")
	})

	t.Run("ListUnmerged", func(t *testing.T) {
		paths, err := sliceutil.CollectErr(
			wt.ListFilesPaths(t.Context(), &git.ListFilesOptions{Unmerged: true}))
		require.NoError(t, err)
		assert.Equal(t, []string{"conflict.txt"}, paths)
	})
}
