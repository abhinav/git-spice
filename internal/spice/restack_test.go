package spice

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

func TestService_Restack_merge(t *testing.T) {
	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		git init
		git config user.name Test
		git config user.email test@example.com
		git config commit.gpgSign false
		git commit --allow-empty -m 'Initial commit'

		# Track a feature branch off main:
		#
		#   main -- F1 (feature)
		git checkout -b feature
		git add feature.txt
		git commit -m 'Add feature file'

		# Advance main after feature was created:
		#
		#   main -- M2
		#    \
		#     F1 (feature)
		git checkout main
		git add main.txt
		git commit -m 'Add main file'

		git checkout feature

		-- feature.txt --
		feature
		-- main.txt --
		main
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	ctx := t.Context()
	wt, err := git.OpenWorktree(ctx, fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)
	repo := wt.Repository()

	mainHash, err := repo.PeelToCommit(ctx, "main")
	require.NoError(t, err)
	oldFeatureHash, err := repo.PeelToCommit(ctx, "feature")
	require.NoError(t, err)

	store := NewMemoryStore(t)
	tx := store.BeginBranchTx()
	require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
		Name:     "feature",
		Base:     "main",
		BaseHash: oldFeatureHash, // stale: predates M2
	}))
	require.NoError(t, tx.Commit(ctx, "setup"))

	svc := NewTestService(repo, wt, store, nil, silogtest.New(t),
		&ServiceOptions{RestackMethod: RestackMethodMerge})

	res, err := svc.Restack(ctx, "feature")
	require.NoError(t, err)
	assert.Equal(t, "main", res.Base)

	// The restack must produce a merge commit at feature's tip,
	// whose second parent is main.
	newFeatureHash, err := repo.PeelToCommit(ctx, "feature")
	require.NoError(t, err)
	assert.NotEqual(t, oldFeatureHash, newFeatureHash,
		"feature head should advance to the merge commit")

	secondParent, err := repo.PeelToCommit(ctx, "feature^2")
	require.NoError(t, err, "merge commit should have a second parent")
	assert.Equal(t, mainHash, secondParent,
		"second parent of the merge should be main")

	firstParent, err := repo.PeelToCommit(ctx, "feature^1")
	require.NoError(t, err)
	assert.Equal(t, oldFeatureHash, firstParent,
		"first parent of the merge should be the old feature tip")

	// The branch now contains main, and feature's own commit survives.
	assert.True(t, repo.IsAncestor(ctx, mainHash, newFeatureHash),
		"feature should contain main after the merge")
	assert.True(t, repo.IsAncestor(ctx, oldFeatureHash, newFeatureHash),
		"feature should still contain its own commit")

	// Recorded base hash updates to main's head.
	updated, err := svc.LookupBranch(ctx, "feature")
	require.NoError(t, err)
	assert.Equal(t, mainHash, updated.BaseHash)
}

func TestService_Restack_merge_conflict(t *testing.T) {
	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		git init
		git config user.name Test
		git config user.email test@example.com
		git config commit.gpgSign false
		git commit --allow-empty -m 'Initial commit'

		# Both feature and main edit the same file with different
		# contents, so merging main into feature conflicts.
		#
		#   main -- M2 (edits shared.txt)
		#    \
		#     F1 (feature, edits shared.txt)
		git checkout -b feature
		cp $WORK/extra/feature-shared.txt shared.txt
		git add shared.txt
		git commit -m 'Edit shared file on feature'

		git checkout main
		cp $WORK/extra/main-shared.txt shared.txt
		git add shared.txt
		git commit -m 'Edit shared file on main'

		git checkout feature

		-- extra/feature-shared.txt --
		feature change
		-- extra/main-shared.txt --
		main change
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	ctx := t.Context()
	wt, err := git.OpenWorktree(ctx, fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)
	repo := wt.Repository()

	featureHash, err := repo.PeelToCommit(ctx, "feature")
	require.NoError(t, err)

	store := NewMemoryStore(t)
	tx := store.BeginBranchTx()
	require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
		Name:     "feature",
		Base:     "main",
		BaseHash: featureHash,
	}))
	require.NoError(t, tx.Commit(ctx, "setup"))

	svc := NewTestService(repo, wt, store, nil, silogtest.New(t),
		&ServiceOptions{RestackMethod: RestackMethodMerge})

	_, err = svc.Restack(ctx, "feature")
	require.Error(t, err)

	var mergeErr *git.MergeInterruptError
	require.ErrorAs(t, err, &mergeErr)
	assert.Equal(t, "feature", mergeErr.State.Branch)

	// Clean up the in-progress merge so the worktree is restored.
	assert.NoError(t, wt.MergeAbort(ctx))
}
