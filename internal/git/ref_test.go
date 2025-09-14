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

func TestSetRef(t *testing.T) {
	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		at '2024-09-14T15:55:40Z'

		git init
		git commit --allow-empty -m 'Initial commit'

		git add feat1.txt
		git commit -m 'Add feat1'

		git add feat2.txt
		git commit -m 'Add feat2'

		git add feat3.txt
		git commit -m 'Add feat3'

		-- feat1.txt --
		Feature 1
		-- feat2.txt --
		Feature 2
		-- feat3.txt --
		Feature 3
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	repo, err := git.Open(t.Context(), fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	ctx := t.Context()
	branches, err := sliceutil.CollectErr(repo.LocalBranches(ctx, nil))
	require.NoError(t, err)
	if assert.Len(t, branches, 1) {
		assert.Equal(t, "main", branches[0].Name)
	}

	feat3Hash, err := repo.PeelToCommit(ctx, "HEAD")
	require.NoError(t, err)

	require.NoError(t, repo.SetRef(ctx, git.SetRefRequest{
		Ref:     "refs/heads/my-feature",
		Hash:    feat3Hash,
		OldHash: git.ZeroHash,
	}))

	branches, err = sliceutil.CollectErr(repo.LocalBranches(ctx, nil))
	require.NoError(t, err)
	if assert.Len(t, branches, 2) {
		names := []string{branches[0].Name, branches[1].Name}
		assert.ElementsMatch(t, []string{"main", "my-feature"}, names)
	}

	branchHead, err := repo.PeelToCommit(ctx, "my-feature")
	require.NoError(t, err)
	assert.Equal(t, feat3Hash, branchHead)

	t.Run("UpdateBranch", func(t *testing.T) {
		feat2Hash, err := repo.PeelToCommit(ctx, "HEAD^")
		require.NoError(t, err)

		err = repo.SetRef(ctx, git.SetRefRequest{
			Ref:     "refs/heads/my-feature",
			Hash:    feat2Hash,
			OldHash: feat3Hash,
			Reason:  "Moving my-feature back to feat2",
		})
		require.NoError(t, err)

		branchHead, err := repo.PeelToCommit(ctx, "my-feature")
		require.NoError(t, err)
		assert.Equal(t, feat2Hash, branchHead)
	})

	t.Run("AlreadyExists", func(t *testing.T) {
		feat1Hash, err := repo.PeelToCommit(ctx, "HEAD^^")
		require.NoError(t, err)

		err = repo.SetRef(ctx, git.SetRefRequest{
			Ref:     "refs/heads/my-feature",
			Hash:    feat1Hash,
			OldHash: git.ZeroHash,
		})
		require.Error(t, err)

		branchHead, err := repo.PeelToCommit(ctx, "my-feature")
		require.NoError(t, err)
		assert.NotEqual(t, feat1Hash, branchHead)
	})
}
