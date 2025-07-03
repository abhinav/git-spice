package git_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/text"
)

func TestIntegrationWorktrees(t *testing.T) {
	t.Parallel()

	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		cd setup-repo

		at '2024-08-27T21:48:32Z'
		git init
		git add init.txt
		git commit -m 'Initial commit'

		git checkout -b feature1
		git add feature1.txt
		git commit -m 'Add feature1'

		git checkout -b feature2
		git add feature2.txt
		git commit -m 'Add feature2'

		git checkout main

		cd ..
		git clone --bare setup-repo bare
		cd bare

		# Create worktree with branch checked out
		git worktree add ../wt-feature1 feature1

		# Create worktree in detached HEAD state
		git worktree add --detach ../wt-detached HEAD

		# Create locked worktree
		git worktree add ../wt-locked feature2
		git worktree lock --reason 'i have my reasons' ../wt-locked

		-- setup-repo/init.txt --
		Initial

		-- setup-repo/feature1.txt --
		Contents of feature1

		-- setup-repo/feature2.txt --
		Contents of feature2

	`)))
	require.NoError(t, err)

	repo, err := git.Open(t.Context(), filepath.Join(fixture.Dir(), "bare"), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	worktrees, err := sliceutil.CollectErr(repo.Worktrees(t.Context()))
	require.NoError(t, err)

	assert.ElementsMatch(t, []*git.WorktreeListItem{
		{
			Path: joinSlash(fixture.Dir(), "bare"),
			Bare: true,
		},
		{
			Path:     joinSlash(fixture.Dir(), "wt-detached"),
			Detached: true,
			Head:     "a5fe281e95373c84144272b944ec2a1c2e82ed62",
		},
		{
			Path:   joinSlash(fixture.Dir(), "wt-feature1"),
			Branch: "feature1",
			Head:   "bbd92eb0593b60e0bb9e923c9cb74f8bc8422c71",
		},
		{
			Path:         joinSlash(fixture.Dir(), "wt-locked"),
			Branch:       "feature2",
			LockedReason: "i have my reasons",
			Head:         "c149b81591a121a1ef9ae4b27bd0c6d43565cea6",
		},
	}, worktrees)
}
