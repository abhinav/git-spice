package git_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/text"
)

func TestWorktree_CheckoutNewBranch(t *testing.T) {
	t.Parallel()

	t.Run("CreateAtHEAD", func(t *testing.T) {
		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			as 'Test <test@example.com>'
			at '2025-01-01T00:00:00Z'

			git init
			git add file.txt
			git commit -m 'Initial commit'

			-- file.txt --
			content
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		require.NoError(t, wt.CheckoutNewBranch(t.Context(), git.CheckoutNewBranchRequest{
			Name: "feature",
		}))

		cur, err := wt.CurrentBranch(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "feature", cur)
	})

	t.Run("CreateAtStartPoint", func(t *testing.T) {
		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			as 'Test <test@example.com>'
			at '2025-01-01T00:00:00Z'

			git init
			git add file.txt
			git commit -m 'Initial commit'

			git checkout -b feature
			git add feature.txt
			git commit -m 'Feature commit'

			git checkout main

			-- file.txt --
			content
			-- feature.txt --
			feature content
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		require.NoError(t, wt.CheckoutNewBranch(t.Context(), git.CheckoutNewBranchRequest{
			Name:       "new-branch",
			StartPoint: "feature",
		}))

		cur, err := wt.CurrentBranch(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "new-branch", cur)
	})

	t.Run("ExistsWithoutForceFails", func(t *testing.T) {
		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			as 'Test <test@example.com>'
			at '2025-01-01T00:00:00Z'

			git init
			git add file.txt
			git commit -m 'Initial commit'

			git checkout -b existing
			git checkout main

			-- file.txt --
			content
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		err = wt.CheckoutNewBranch(t.Context(), git.CheckoutNewBranchRequest{
			Name: "existing",
		})
		require.Error(t, err)
	})

	t.Run("ForceResetsExisting", func(t *testing.T) {
		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			as 'Test <test@example.com>'
			at '2025-01-01T00:00:00Z'

			git init
			git add file.txt
			git commit -m 'Initial commit'

			git checkout -b existing
			git add new.txt
			git commit -m 'On existing'

			git checkout main

			-- file.txt --
			content
			-- new.txt --
			more content
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		require.NoError(t, wt.CheckoutNewBranch(t.Context(), git.CheckoutNewBranchRequest{
			Name:       "existing",
			StartPoint: "main",
			Force:      true,
		}))

		cur, err := wt.CurrentBranch(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "existing", cur)
	})

	t.Run("EmptyNameFails", func(t *testing.T) {
		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			git init
			git commit --allow-empty -m 'Initial commit'
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err)

		err = wt.CheckoutNewBranch(t.Context(), git.CheckoutNewBranchRequest{Name: ""})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name is required")
	})
}
