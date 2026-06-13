package git_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/text"
)

func TestWorktree_Merge_noFF(t *testing.T) {
	t.Parallel()

	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		at '2025-01-01T00:00:00Z'

		git init
		git config user.name 'Test'
		git config user.email 'test@example.com'
		git add base.txt
		git commit -m 'Initial commit'

		git checkout -b feature
		git add feature.txt
		git commit -m 'Add feature'

		git checkout main

		-- base.txt --
		base content
		-- feature.txt --
		feature content
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	require.NoError(t, wt.Merge(t.Context(), git.MergeOptions{
		Refs:    []string{"feature"},
		NoFF:    true,
		Message: "Merge feature",
	}))

	// HEAD should now have feature.txt.
	content, err := os.ReadFile(filepath.Join(fixture.Dir(), "feature.txt"))
	require.NoError(t, err)
	assert.Equal(t, "feature content", strings.TrimSpace(string(content)))
}

func TestWorktree_Merge_conflict(t *testing.T) {
	t.Parallel()

	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		at '2025-01-01T00:00:00Z'

		git init
		git config user.name 'Test'
		git config user.email 'test@example.com'
		git add shared.txt
		git commit -m 'Initial commit'

		git checkout -b feature
		cp feature.txt shared.txt
		git add shared.txt
		git commit -m 'Feature changes shared.txt'

		git checkout main
		cp main.txt shared.txt
		git add shared.txt
		git commit -m 'Main changes shared.txt'

		-- shared.txt --
		base content
		-- feature.txt --
		feature content
		-- main.txt --
		main content
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	err = wt.Merge(t.Context(), git.MergeOptions{
		Refs:    []string{"feature"},
		NoFF:    true,
		Message: "Merge feature",
	})

	var conflict *git.MergeConflictError
	require.Error(t, err)
	require.True(t, errors.As(err, &conflict), "want MergeConflictError, got %T: %v", err, err)
	assert.Equal(t, []string{"feature"}, conflict.Refs)
	assert.Equal(t, []string{"shared.txt"}, conflict.ConflictPaths)

	// Merge must have been aborted: worktree is clean.
	clean, err := wt.IsClean(t.Context())
	require.NoError(t, err)
	assert.True(t, clean, "worktree should be clean after aborted merge")
}

func TestWorktree_Merge_conflictLeaveInWorktree(t *testing.T) {
	t.Parallel()

	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		at '2025-01-01T00:00:00Z'

		git init
		git config user.name 'Test'
		git config user.email 'test@example.com'
		git add shared.txt
		git commit -m 'Initial commit'

		git checkout -b feature
		cp feature.txt shared.txt
		git add shared.txt
		git commit -m 'Feature changes shared.txt'

		git checkout main
		cp main.txt shared.txt
		git add shared.txt
		git commit -m 'Main changes shared.txt'

		-- shared.txt --
		base content
		-- feature.txt --
		feature content
		-- main.txt --
		main content
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	err = wt.Merge(t.Context(), git.MergeOptions{
		Refs:          []string{"feature"},
		NoFF:          true,
		Message:       "Merge feature",
		LeaveConflict: true,
	})

	var conflict *git.MergeConflictError
	require.Error(t, err)
	require.True(t, errors.As(err, &conflict))

	// The merge was NOT aborted; the worktree is left in conflict
	// state for the caller to resolve.
	clean, err := wt.IsClean(t.Context())
	require.NoError(t, err)
	assert.False(t, clean, "worktree should remain in conflict state when LeaveConflict is set")

	require.NoError(t, wt.MergeAbort(t.Context()))
}

func TestWorktree_MergeContinue(t *testing.T) {
	t.Parallel()

	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		at '2025-01-01T00:00:00Z'

		git init
		git add shared.txt
		git commit -m 'Initial commit'

		git checkout -b feature
		cp feature.txt shared.txt
		git add shared.txt
		git commit -m 'Feature changes shared.txt'

		git checkout main
		cp main.txt shared.txt
		git add shared.txt
		git commit -m 'Main changes shared.txt'

		-- shared.txt --
		base content
		-- feature.txt --
		feature content
		-- main.txt --
		main content
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	mergeErr := wt.Merge(t.Context(), git.MergeOptions{
		Refs:          []string{"feature"},
		NoFF:          true,
		Message:       "Merge feature",
		LeaveConflict: true,
	})
	require.Error(t, mergeErr)
	require.True(t, errors.As(mergeErr, new(*git.MergeConflictError)))

	// Simulate an external resolver writing a resolution.
	require.NoError(t,
		os.WriteFile(filepath.Join(fixture.Dir(), "shared.txt"),
			[]byte("resolved content\n"), 0o600))

	require.NoError(t, wt.MergeContinue(t.Context(),
		[]string{"shared.txt"}, "Merge feature"))

	clean, err := wt.IsClean(t.Context())
	require.NoError(t, err)
	assert.True(t, clean, "worktree should be clean after MergeContinue")

	content, err := os.ReadFile(filepath.Join(fixture.Dir(), "shared.txt"))
	require.NoError(t, err)
	assert.Equal(t, "resolved content", strings.TrimSpace(string(content)))
}

func TestWorktree_MergeContinue_unmergedRemain(t *testing.T) {
	t.Parallel()

	// Set up a merge that conflicts on TWO files; pass only one to
	// MergeContinue. The other remains unmerged and MergeContinue
	// must refuse to commit.
	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		at '2025-01-01T00:00:00Z'

		git init
		git add a.txt b.txt
		git commit -m 'Initial commit'

		git checkout -b feature
		cp a-feature.txt a.txt
		cp b-feature.txt b.txt
		git add a.txt b.txt
		git commit -m 'feature edits a and b'

		git checkout main
		cp a-main.txt a.txt
		cp b-main.txt b.txt
		git add a.txt b.txt
		git commit -m 'main edits a and b'

		-- a.txt --
		base a
		-- b.txt --
		base b
		-- a-feature.txt --
		feature a
		-- b-feature.txt --
		feature b
		-- a-main.txt --
		main a
		-- b-main.txt --
		main b
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	mergeErr := wt.Merge(t.Context(), git.MergeOptions{
		Refs:          []string{"feature"},
		NoFF:          true,
		Message:       "Merge feature",
		LeaveConflict: true,
	})
	require.Error(t, mergeErr)
	require.True(t, errors.As(mergeErr, new(*git.MergeConflictError)))

	// Resolve only a.txt; leave b.txt unresolved.
	require.NoError(t,
		os.WriteFile(filepath.Join(fixture.Dir(), "a.txt"),
			[]byte("resolved a\n"), 0o600))

	err = wt.MergeContinue(t.Context(),
		[]string{"a.txt"}, "Merge feature")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmerged paths remain")

	require.NoError(t, wt.MergeAbort(t.Context()))
}

func TestWorktree_Merge_noRefs(t *testing.T) {
	t.Parallel()

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

	err = wt.Merge(t.Context(), git.MergeOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one ref")
}
