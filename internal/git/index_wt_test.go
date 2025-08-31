package git_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/text"
)

func TestWorktree_WriteIndexTree(t *testing.T) {
	t.Parallel()

	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		at '2025-08-30T21:28:29Z'

		git init
		git commit --allow-empty -m 'Initial commit'

		git add file1.txt file2.txt
		git add subdir/file3.txt

		-- file1.txt --
		content of file 1
		-- file2.txt --
		content of file 2
		-- subdir/file3.txt --
		content of file 3
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	ctx := t.Context()
	worktree, err := git.OpenWorktree(ctx, fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	repo := worktree.Repository()

	// WriteIndexTree writes whatever's in the index
	// to a new tree object and returns its hash.
	treeHash, err := worktree.WriteIndexTree(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, treeHash)

	readFile := func(path string) string {
		hash, err := repo.HashAt(ctx, treeHash.String(), path)
		require.NoError(t, err)

		var buf bytes.Buffer
		err = repo.ReadObject(ctx, git.BlobType, hash, &buf)
		require.NoError(t, err)

		return buf.String()
	}

	assert.Equal(t, "content of file 1\n", readFile("file1.txt"))
	assert.Equal(t, "content of file 2\n", readFile("file2.txt"))
	assert.Equal(t, "content of file 3\n", readFile("subdir/file3.txt"))
}
