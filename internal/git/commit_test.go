package git_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/text"
)

func TestCommitAheadBehind(t *testing.T) {
	t.Parallel()

	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		at '2025-03-16T18:19:20Z'

		cd upstream
		git init
		git commit --allow-empty -m 'Initial commit'

		git checkout -b feat1
		git add feat1.txt
		git commit -m 'Add feat1'
		git branch feat2
		git branch feat3
		git branch feat4

		cd ..
		git clone upstream fork
		cd fork

		git checkout feat1
		git checkout feat3

		git checkout feat2
		cp $WORK/extra/feat2.txt .
		git add feat2.txt
		git commit -m 'Add feat2'

		cd ../upstream
		git checkout feat3
		git add feat3.txt
		git commit -m 'Add feat3'

		git checkout feat4
		git add feat4.txt
		git commit -m 'Add feat4'

		cd ../fork
		git checkout feat4
		cp $WORK/extra/feat4.txt .
		git add feat4.txt
		git commit -m 'Add feat4-fork'
		git fetch

		-- upstream/feat1.txt --
		feat1
		-- upstream/feat3.txt --
		feat3
		-- upstream/feat4.txt --
		feat4
		-- extra/feat2.txt --
		feat2
		-- extra/feat4.txt --
		feat4-fork
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	// From the point of view of the fork:
	//
	//   - feat1 is in sync with upstream
	//   - feat2 is ahead by 1 commit (the one we just made)
	//   - feat3 is behind by 1 commit (the one we just made in upstream)
	//   - feat4 is ahead and behind by 1 commit
	ctx := t.Context()
	fork, err := git.Open(ctx, filepath.Join(fixture.Dir(), "fork"), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	t.Run("Synced", func(t *testing.T) {
		t.Parallel()

		ahead, behind, err := fork.CommitAheadBehind(ctx, "origin/feat1", "feat1")
		require.NoError(t, err)
		assert.Zero(t, ahead, "expected 0 commits ahead")
		assert.Zero(t, behind, "expected 0 commits behind")
	})

	t.Run("Ahead", func(t *testing.T) {
		t.Parallel()

		ahead, behind, err := fork.CommitAheadBehind(ctx, "origin/feat2", "feat2")
		require.NoError(t, err)
		assert.Equal(t, 1, ahead, "expected 1 commit ahead")
		assert.Zero(t, behind, "expected 0 commits behind")
	})

	t.Run("Behind", func(t *testing.T) {
		t.Parallel()

		ahead, behind, err := fork.CommitAheadBehind(ctx, "origin/feat3", "feat3")
		require.NoError(t, err)
		assert.Zero(t, ahead, "expected 0 commits ahead")
		assert.Equal(t, 1, behind, "expected 1 commit behind")
	})

	t.Run("Both", func(t *testing.T) {
		t.Parallel()

		ahead, behind, err := fork.CommitAheadBehind(ctx, "origin/feat4", "feat4")
		require.NoError(t, err)
		assert.Equal(t, 1, ahead, "expected 1 commit ahead")
		assert.Equal(t, 1, behind, "expected 1 commit behind")
	})
}
