package git_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/text"
)

func TestIntegrationBranches(t *testing.T) {
	t.Parallel()

	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		at '2024-08-27T21:48:32Z'
		git init
		git add init.txt
		git commit -m 'Initial commit'

		at '2024-08-27T21:50:12Z'
		git checkout -b feature1
		git add feature1.txt
		git commit -m 'Add feature1'

		at '2024-08-27T21:52:12Z'
		git checkout -b feature2
		git add feature2.txt
		git commit -m 'Add feature2'

		git checkout main

		-- init.txt --
		Initial

		-- feature1.txt --
		Contents of feature1

		-- feature2.txt --
		Contents of feature2

	`)))
	require.NoError(t, err)

	wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)
	repo := wt.Repository()

	t.Run("CurrentBranch", func(t *testing.T) {
		name, err := wt.CurrentBranch(t.Context())
		require.NoError(t, err)

		assert.Equal(t, "main", name)
	})

	t.Run("ListBranches", func(t *testing.T) {
		bs, err := sliceutil.CollectErr(repo.LocalBranches(t.Context(), nil))
		require.NoError(t, err)

		assert.Equal(t, []git.LocalBranch{
			{Name: "feature1", Hash: "0a08a7c2b265465f4ae02291fad1d5723877a20e"},
			{Name: "feature2", Hash: "8ab6a1b8262f1f0d9af261e07381888148bdb092"},
			{Name: "main", Hash: "a5fe281e95373c84144272b944ec2a1c2e82ed62", Worktree: joinSlash(fixture.Dir())},
		}, bs)
	})

	t.Run("ListBranchesSorted", func(t *testing.T) {
		bs, err := sliceutil.CollectErr(repo.LocalBranches(t.Context(), &git.LocalBranchesOptions{
			Sort: "committerdate",
		}))
		require.NoError(t, err)

		assert.Equal(t, []git.LocalBranch{
			{Name: "main", Hash: "a5fe281e95373c84144272b944ec2a1c2e82ed62", Worktree: joinSlash(fixture.Dir())},
			{Name: "feature1", Hash: "0a08a7c2b265465f4ae02291fad1d5723877a20e"},
			{Name: "feature2", Hash: "8ab6a1b8262f1f0d9af261e07381888148bdb092"},
		}, bs)
	})

	backToMain := func(t testing.TB) {
		t.Helper()

		assert.NoError(t, wt.CheckoutBranch(t.Context(), "main"))
	}

	t.Run("Checkout", func(t *testing.T) {
		defer backToMain(t)

		require.NoError(t, wt.CheckoutBranch(t.Context(), "feature1"))

		name, err := wt.CurrentBranch(t.Context())
		require.NoError(t, err)

		assert.Equal(t, "feature1", name)
	})

	t.Run("DetachedHead", func(t *testing.T) {
		defer backToMain(t)

		require.NoError(t, wt.DetachHead(t.Context(), "main"))

		_, err := wt.CurrentBranch(t.Context())
		assert.ErrorIs(t, err, git.ErrDetachedHead)
	})

	t.Run("CreateBranch", func(t *testing.T) {
		require.NoError(t, repo.CreateBranch(t.Context(), git.CreateBranchRequest{
			Name: "feature3",
			Head: "main",
		}))

		bs, err := sliceutil.CollectErr(repo.LocalBranches(t.Context(), nil))
		if assert.NoError(t, err) {
			assert.Equal(t, []git.LocalBranch{
				{Name: "feature1", Hash: "0a08a7c2b265465f4ae02291fad1d5723877a20e"},
				{Name: "feature2", Hash: "8ab6a1b8262f1f0d9af261e07381888148bdb092"},
				{Name: "feature3", Hash: "a5fe281e95373c84144272b944ec2a1c2e82ed62"},
				{Name: "main", Hash: "a5fe281e95373c84144272b944ec2a1c2e82ed62", Worktree: joinSlash(fixture.Dir())},
			}, bs)
		}

		t.Run("DeleteBranch", func(t *testing.T) {
			require.NoError(t,
				repo.DeleteBranch(t.Context(), "feature3", git.BranchDeleteOptions{
					Force: true,
				}))

			bs, err := sliceutil.CollectErr(repo.LocalBranches(t.Context(), nil))
			require.NoError(t, err)

			assert.Equal(t, []git.LocalBranch{
				{Name: "feature1", Hash: "0a08a7c2b265465f4ae02291fad1d5723877a20e"},
				{Name: "feature2", Hash: "8ab6a1b8262f1f0d9af261e07381888148bdb092"},
				{Name: "main", Hash: "a5fe281e95373c84144272b944ec2a1c2e82ed62", Worktree: joinSlash(fixture.Dir())},
			}, bs)
		})
	})

	t.Run("RenameBranch", func(t *testing.T) {
		require.NoError(t, repo.CreateBranch(t.Context(), git.CreateBranchRequest{
			Name: "feature3",
			Head: "main",
		}))

		require.NoError(t, repo.RenameBranch(t.Context(), git.RenameBranchRequest{
			OldName: "feature3",
			NewName: "feature4",
		}))

		bs, err := sliceutil.CollectErr(repo.LocalBranches(t.Context(), nil))
		if assert.NoError(t, err) {
			assert.Equal(t, []git.LocalBranch{
				{Name: "feature1", Hash: "0a08a7c2b265465f4ae02291fad1d5723877a20e"},
				{Name: "feature2", Hash: "8ab6a1b8262f1f0d9af261e07381888148bdb092"},
				{Name: "feature4", Hash: "a5fe281e95373c84144272b944ec2a1c2e82ed62"},
				{Name: "main", Hash: "a5fe281e95373c84144272b944ec2a1c2e82ed62", Worktree: joinSlash(fixture.Dir())},
			}, bs)
		}

		require.NoError(t,
			repo.DeleteBranch(t.Context(), "feature4", git.BranchDeleteOptions{
				Force: true,
			}))
	})

	t.Run("ListBranchesWithPatterns", func(t *testing.T) {
		for _, branch := range []string{"feature/branch1", "feature/branch2", "bugfix/test", "release-1.0"} {
			require.NoError(t, repo.CreateBranch(t.Context(), git.CreateBranchRequest{
				Name: branch,
				Head: "main",
			}))

			defer func() {
				assert.NoError(t, repo.DeleteBranch(
					t.Context(), branch, git.BranchDeleteOptions{Force: true}),
				)
			}()
		}

		t.Run("LiteralBranchName", func(t *testing.T) {
			bs, err := sliceutil.CollectErr(repo.LocalBranches(t.Context(), &git.LocalBranchesOptions{
				Patterns: []string{"main"},
			}))
			require.NoError(t, err)
			assert.Equal(t, []git.LocalBranch{
				{Name: "main", Hash: "a5fe281e95373c84144272b944ec2a1c2e82ed62", Worktree: joinSlash(fixture.Dir())},
			}, bs)
		})

		t.Run("PrefixPattern", func(t *testing.T) {
			bs, err := sliceutil.CollectErr(repo.LocalBranches(t.Context(), &git.LocalBranchesOptions{
				Patterns: []string{"feature/"},
			}))
			require.NoError(t, err)
			assert.Equal(t, []git.LocalBranch{
				{Name: "feature/branch1", Hash: "a5fe281e95373c84144272b944ec2a1c2e82ed62"},
				{Name: "feature/branch2", Hash: "a5fe281e95373c84144272b944ec2a1c2e82ed62"},
			}, bs)
		})

		t.Run("GlobPattern", func(t *testing.T) {
			bs, err := sliceutil.CollectErr(repo.LocalBranches(t.Context(), &git.LocalBranchesOptions{
				Patterns: []string{"feature*"},
			}))
			require.NoError(t, err)
			assert.Equal(t, []git.LocalBranch{
				{Name: "feature1", Hash: "0a08a7c2b265465f4ae02291fad1d5723877a20e"},
				{Name: "feature2", Hash: "8ab6a1b8262f1f0d9af261e07381888148bdb092"},
			}, bs)
		})

		t.Run("MultiplePatterns", func(t *testing.T) {
			bs, err := sliceutil.CollectErr(repo.LocalBranches(t.Context(), &git.LocalBranchesOptions{
				Patterns: []string{"main", "feature/"},
			}))
			require.NoError(t, err)
			assert.Equal(t, []git.LocalBranch{
				{Name: "feature/branch1", Hash: "a5fe281e95373c84144272b944ec2a1c2e82ed62"},
				{Name: "feature/branch2", Hash: "a5fe281e95373c84144272b944ec2a1c2e82ed62"},
				{Name: "main", Hash: "a5fe281e95373c84144272b944ec2a1c2e82ed62", Worktree: joinSlash(fixture.Dir())},
			}, bs)
		})

		t.Run("NoMatches", func(t *testing.T) {
			bs, err := sliceutil.CollectErr(repo.LocalBranches(t.Context(), &git.LocalBranchesOptions{
				Patterns: []string{"nonexistent"},
			}))
			require.NoError(t, err)
			assert.Empty(t, bs)
		})
	})
}

func TestIntegrationLocalBranchesWorktrees(t *testing.T) {
	t.Parallel()

	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		cd repo

		at '2024-08-27T21:48:32Z'
		git init
		git add init.txt
		git commit -m 'Initial commit'

		at '2024-08-27T21:50:12Z'
		git checkout -b feature1
		git add feature1.txt
		git commit -m 'Add feature1'

		at '2024-08-27T21:52:12Z'
		git checkout -b feature2
		git add feature2.txt
		git commit -m 'Add feature2'

		git checkout main

		git worktree add ../wt1 feature1

		-- repo/init.txt --
		Initial

		-- repo/feature1.txt --
		Contents of feature1

		-- repo/feature2.txt --
		Contents of feature2

	`)))
	require.NoError(t, err)

	ctx := t.Context()
	repo, err := git.Open(ctx,
		filepath.Join(fixture.Dir(), "repo"),
		git.OpenOptions{Log: silogtest.New(t)},
	)
	require.NoError(t, err)

	bs, err := sliceutil.CollectErr(repo.LocalBranches(ctx, nil))
	require.NoError(t, err)

	assert.Equal(t, []git.LocalBranch{
		{Name: "feature1", Hash: "0a08a7c2b265465f4ae02291fad1d5723877a20e", Worktree: joinSlash(fixture.Dir(), "wt1")},
		{Name: "feature2", Hash: "8ab6a1b8262f1f0d9af261e07381888148bdb092"},
		{Name: "main", Hash: "a5fe281e95373c84144272b944ec2a1c2e82ed62", Worktree: joinSlash(fixture.Dir(), "repo")},
	}, bs)
}

func TestIntegrationRemoteBranches(t *testing.T) {
	t.Parallel()

	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		cd repo

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
		git clone repo clone
		cd clone
		git checkout -b feature1

		-- repo/init.txt --
		Initial

		-- repo/feature1.txt --
		Contents of feature1

		-- repo/feature2.txt --
		Contents of feature2

	`)))
	require.NoError(t, err)

	repo, err := git.Open(t.Context(),
		filepath.Join(fixture.Dir(), "clone"),
		git.OpenOptions{Log: silogtest.New(t)},
	)
	require.NoError(t, err)

	t.Run("no upstream", func(t *testing.T) {
		_, err := repo.BranchUpstream(t.Context(), "feature1")
		require.Error(t, err)
		assert.ErrorIs(t, err, git.ErrNotExist)
	})

	require.NoError(t,
		repo.SetBranchUpstream(t.Context(), "feature1", "origin/feature1"))

	t.Run("has upstream", func(t *testing.T) {
		upstream, err := repo.BranchUpstream(t.Context(), "feature1")
		require.NoError(t, err)
		assert.Equal(t, "origin/feature1", upstream)
	})

	t.Run("unset upstream", func(t *testing.T) {
		require.NoError(t,
			repo.SetBranchUpstream(t.Context(), "feature1", ""))

		_, err := repo.BranchUpstream(t.Context(), "feature1")
		require.Error(t, err)
		assert.ErrorIs(t, err, git.ErrNotExist)
	})

	require.NoError(t,
		repo.SetBranchUpstream(t.Context(), "feature1", "origin/feature1"))

	t.Run("delete upstream", func(t *testing.T) {
		require.NoError(t,
			repo.DeleteBranch(t.Context(), "origin/feature1", git.BranchDeleteOptions{
				Remote: true,
			}))
		_, err := repo.BranchUpstream(t.Context(), "feature1")
		require.Error(t, err)
		assert.ErrorIs(t, err, git.ErrNotExist)
	})
}

// BranchExists must not log to stderr when branch does not exist.
// It's a read operation.
func TestBranchExists_doesNotLog(t *testing.T) {
	t.Parallel()

	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		git init
		git add init.txt
		git commit -m 'Initial commit'

		-- init.txt --
		Initial
	`)))
	require.NoError(t, err)

	var logBuffer bytes.Buffer
	repo, err := git.Open(t.Context(), fixture.Dir(), git.OpenOptions{
		Log: silog.New(&logBuffer, &silog.Options{
			Level: silog.LevelDebug,
		}),
	})
	require.NoError(t, err)

	assert.True(t, repo.BranchExists(t.Context(), "main"))
	assert.Empty(t, logBuffer.String())

	assert.False(t, repo.BranchExists(t.Context(), "nonexistent"))
	assert.Empty(t, logBuffer.String())
}

// joinSlash joins the given paths and converts it to slash-separated path.
//
// Use this when the result is always /-separated, e.g. for git paths.
func joinSlash(paths ...string) string {
	return filepath.ToSlash(filepath.Join(paths...))
}
