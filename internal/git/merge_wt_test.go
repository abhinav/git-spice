package git_test

import (
	"errors"
	"os"
	"os/exec"
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

func TestMerge_clean(t *testing.T) {
	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		at '2024-05-21T20:30:40Z'

		git init
		git add base.txt
		git commit -m 'Initial commit'

		git checkout -b feature
		git add feature.txt
		git commit -m 'Add feature'

		git checkout main
		git add other.txt
		git commit -m 'Add other'

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

	headBefore, err := wt.Head(ctx)
	require.NoError(t, err)

	require.NoError(t, wt.Merge(ctx, git.MergeRequest{
		Commit:  "main",
		Message: "Merge main into feature",
	}))

	// A merge commit should have been created.
	headAfter, err := wt.Head(ctx)
	require.NoError(t, err)
	assert.NotEqual(t, headBefore, headAfter)

	// No merge should be in progress anymore.
	_, err = wt.MergeState(ctx)
	assert.ErrorIs(t, err, git.ErrNoMerge)

	// The merged-in file should be present in the worktree.
	assert.FileExists(t, filepath.Join(fixture.Dir(), "other.txt"))
}

func TestMerge_conflictResolveContinue(t *testing.T) {
	wt, dir := conflictingWorktree(t)

	ctx := t.Context()

	err := wt.Merge(ctx, git.MergeRequest{
		Commit:  "main",
		Message: "Merge main into feature",
	})
	require.Error(t, err)

	var mergeErr *git.MergeInterruptError
	require.ErrorAs(t, err, &mergeErr)
	require.NotNil(t, mergeErr.State)
	assert.Equal(t, "feature", mergeErr.State.Branch)
	assert.NotEmpty(t, mergeErr.State.Head)

	// MergeState reports the in-progress merge.
	state, err := wt.MergeState(ctx)
	require.NoError(t, err)
	assert.Equal(t, "feature", state.Branch)

	// Resolve the conflict and stage it.
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "shared.txt"),
		[]byte("Resolved version of shared"), 0o644))
	addCmd := exec.Command("git", "add", "shared.txt")
	addCmd.Dir = dir
	require.NoError(t, addCmd.Run())

	require.NoError(t, wt.MergeContinue(ctx, nil))

	// No merge should be in progress anymore.
	_, err = wt.MergeState(ctx)
	assert.ErrorIs(t, err, git.ErrNoMerge)

	// The resolved content should be on disk.
	bs, err := os.ReadFile(filepath.Join(dir, "shared.txt"))
	require.NoError(t, err)
	assert.Equal(t, "Resolved version of shared", string(bs))
}

func TestMerge_abort(t *testing.T) {
	wt, _ := conflictingWorktree(t)

	ctx := t.Context()

	headBefore, err := wt.Head(ctx)
	require.NoError(t, err)

	err = wt.Merge(ctx, git.MergeRequest{
		Commit:  "main",
		Message: "Merge main into feature",
	})
	require.ErrorAs(t, err, new(*git.MergeInterruptError))

	require.NoError(t, wt.MergeAbort(ctx))

	// The merge is cleared and HEAD is restored.
	_, err = wt.MergeState(ctx)
	assert.ErrorIs(t, err, git.ErrNoMerge)

	headAfter, err := wt.Head(ctx)
	require.NoError(t, err)
	assert.Equal(t, headBefore, headAfter)
}

func TestMerge_rerereScopedPerInvocation(t *testing.T) {
	wt, dir := conflictingWorktree(t)

	ctx := t.Context()

	err := wt.Merge(ctx, git.MergeRequest{
		Commit:  "main",
		Message: "Merge main into feature",
		Rerere:  true,
	})
	require.ErrorAs(t, err, new(*git.MergeInterruptError))
	defer func() {
		assert.NoError(t, wt.MergeAbort(ctx))
	}()

	// rerere.enabled must NOT be persisted to the repo config:
	// the setting is scoped to the single invocation only.
	cfgCmd := exec.Command("git", "config", "--get", "rerere.enabled")
	cfgCmd.Dir = dir
	out, err := cfgCmd.Output()
	assert.Error(t, err,
		"rerere.enabled should be unset in repo config, got %q", out)
}

func TestMerge_strategyOptionTheirs(t *testing.T) {
	wt, dir := conflictingWorktree(t)

	ctx := t.Context()

	// With -X theirs, the textual conflict resolves automatically
	// in favor of the merged-in branch (main).
	require.NoError(t, wt.Merge(ctx, git.MergeRequest{
		Commit:         "main",
		Message:        "Merge main into feature",
		StrategyOption: "theirs",
	}))

	_, err := wt.MergeState(ctx)
	assert.ErrorIs(t, err, git.ErrNoMerge)

	// "theirs" wins, so main's content replaces feature's.
	// Trim to stay robust against platform line endings.
	bs, err := os.ReadFile(filepath.Join(dir, "shared.txt"))
	require.NoError(t, err)
	assert.Equal(t, "Main version of shared", strings.TrimSpace(string(bs)))
}

func TestMerge_strategyOptionOurs(t *testing.T) {
	wt, dir := conflictingWorktree(t)

	ctx := t.Context()

	// With -X ours, the textual conflict resolves automatically
	// in favor of the current branch (feature).
	require.NoError(t, wt.Merge(ctx, git.MergeRequest{
		Commit:         "main",
		Message:        "Merge main into feature",
		StrategyOption: "ours",
	}))

	_, err := wt.MergeState(ctx)
	assert.ErrorIs(t, err, git.ErrNoMerge)

	// "ours" wins, so feature's content is preserved.
	// Trim to stay robust against platform line endings.
	bs, err := os.ReadFile(filepath.Join(dir, "shared.txt"))
	require.NoError(t, err)
	assert.Equal(t, "Feature version of shared", strings.TrimSpace(string(bs)))
}

func TestInterruptError(t *testing.T) {
	t.Run("MergeInterruptError", func(t *testing.T) {
		err := &git.MergeInterruptError{
			State: &git.MergeState{Branch: "feature"},
		}

		ic, ok := errors.AsType[git.InterruptError](err)
		require.True(t, ok)
		assert.Equal(t, "feature", ic.InterruptedBranch())
	})

	t.Run("RebaseInterruptError", func(t *testing.T) {
		err := &git.RebaseInterruptError{
			State: &git.RebaseState{Branch: "topic"},
		}

		ic, ok := errors.AsType[git.InterruptError](err)
		require.True(t, ok)
		assert.Equal(t, "topic", ic.InterruptedBranch())
	})

	t.Run("NotAnInterrupt", func(t *testing.T) {
		_, ok := errors.AsType[git.InterruptError](assert.AnError)
		assert.False(t, ok)
	})
}

// conflictingWorktree creates a repository where branch 'feature'
// (checked out) and branch 'main' have both edited shared.txt,
// so merging main into feature conflicts.
func conflictingWorktree(t *testing.T) (wt *git.Worktree, dir string) {
	t.Helper()

	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		at '2024-05-21T20:30:40Z'

		git init
		git add base.txt
		git commit -m 'Initial commit'

		git checkout -b feature
		mv feature-shared.txt shared.txt
		git add shared.txt
		git commit -m 'Feature changes shared'

		git checkout main
		mv main-shared.txt shared.txt
		git add shared.txt
		git commit -m 'Main changes shared'

		git checkout feature

		-- base.txt --
		Base content

		-- feature-shared.txt --
		Feature version of shared

		-- main-shared.txt --
		Main version of shared
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	wt, err = git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)
	login(t, "foo")

	return wt, fixture.Dir()
}
