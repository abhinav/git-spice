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

func TestService_BranchOnto_skipRebasePreservesActualBaseBoundary(t *testing.T) {
	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		git init
		git config user.name Test
		git config user.email test@example.com
		git config commit.gpgSign false
		git commit --allow-empty -m 'Initial commit'

		# Create the original downstack branch:
		#
		#   main -- B1 (base)
		git add base.txt
		git commit -m 'Add base file'
		git branch base

		# Create an upstack branch from the original base:
		#
		#   main -- B1 (base) -- S1 (stacked)
		git checkout -b stacked
		git add stacked.txt
		git commit -m 'Add stacked file'

		# Advance the downstack branch after stacked exists:
		#
		#   main -- B1 -- B2 -- B3 (base)
		#            \
		#             S1 (stacked)
		#
		# The test will seed git-spice state from this graph, so stacked's
		# recorded base hash honestly reflects B1 at that point.
		git checkout base
		cp $WORK/extra/base-v2.txt base.txt
		git add base.txt
		git commit -m 'Update base file'

		cp $WORK/extra/base-v3.txt base.txt
		git add base.txt
		git commit -m 'Update base file again'

		# Leave stacked checked out. The test will rebase it outside
		# git-spice after recording the state above.
		git checkout stacked

		-- base.txt --
		base v1
		-- stacked.txt --
		stacked
		-- extra/base-v2.txt --
		base v2
		-- extra/base-v3.txt --
		base v3
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	ctx := t.Context()
	wt, err := git.OpenWorktree(ctx, fixture.Dir(), git.OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)
	repo := wt.Repository()

	oldBaseHash, err := repo.PeelToCommit(ctx, "base~2")
	require.NoError(t, err)
	actualBaseHash, err := repo.PeelToCommit(ctx, "base")
	require.NoError(t, err)

	store := NewMemoryStore(t)
	tx := store.BeginBranchTx()
	require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
		Name:     "base",
		Base:     "main",
		BaseHash: "main",
	}))
	require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
		Name:     "stacked",
		Base:     "base",
		BaseHash: oldBaseHash,
	}))
	require.NoError(t, tx.Commit(ctx, "setup"))

	svc := NewTestService(repo, wt, store, nil, silogtest.New(t), nil)

	// Simulate the user rebasing stacked with plain Git after git-spice
	// recorded its state. stacked now contains B2 and B3, but git-spice
	// still records B1 as the base hash:
	//
	//   main -- B1 -- B2 -- B3 (base) -- S1' (stacked)
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	require.NoError(t, wt.Rebase(ctx, git.RebaseRequest{
		Branch:   "stacked",
		Upstream: oldBaseHash.String(),
		Onto:     actualBaseHash.String(),
		Quiet:    true,
	}))

	require.NoError(t, svc.BranchOnto(ctx, &BranchOntoRequest{
		Branch: "stacked",
		Onto:   "main",
		Mode:   BranchOntoRetargetOnly,
	}))

	retargeted, err := svc.LookupBranch(ctx, "stacked")
	require.NoError(t, err)
	assert.Equal(t, "main", retargeted.Base)
	assert.Equal(t, actualBaseHash, retargeted.BaseHash)

	_, err = svc.Restack(ctx, "stacked")
	require.NoError(t, err)

	messages, err := repo.CommitMessageRange(ctx, "stacked", "main")
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, "Add stacked file", messages[0].Subject)
}

func TestService_BranchOnto_merge(t *testing.T) {
	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		git init
		git config user.name Test
		git config user.email test@example.com
		git config commit.gpgSign false
		git commit --allow-empty -m 'Initial commit'

		# Two sibling branches off main:
		#
		#   main -- B1 (newbase)
		#    \
		#     F1 (feature)
		git checkout -b newbase
		git add newbase.txt
		git commit -m 'Add newbase file'

		git checkout main
		git checkout -b feature
		git add feature.txt
		git commit -m 'Add feature file'

		-- newbase.txt --
		newbase
		-- feature.txt --
		feature
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
	newBaseHash, err := repo.PeelToCommit(ctx, "newbase")
	require.NoError(t, err)
	oldFeatureHash, err := repo.PeelToCommit(ctx, "feature")
	require.NoError(t, err)

	store := NewMemoryStore(t)
	tx := store.BeginBranchTx()
	require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
		Name:     "newbase",
		Base:     "main",
		BaseHash: mainHash,
	}))
	require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
		Name:     "feature",
		Base:     "main",
		BaseHash: mainHash,
	}))
	require.NoError(t, tx.Commit(ctx, "setup"))

	svc := NewTestService(repo, wt, store, nil, silogtest.New(t),
		&ServiceOptions{RestackMethod: RestackMethodMerge})

	require.NoError(t, svc.BranchOnto(ctx, &BranchOntoRequest{
		Branch: "feature",
		Onto:   "newbase",
	}))

	// A merge commit lands on feature with newbase as the second parent.
	newFeatureHash, err := repo.PeelToCommit(ctx, "feature")
	require.NoError(t, err)
	assert.NotEqual(t, oldFeatureHash, newFeatureHash)

	secondParent, err := repo.PeelToCommit(ctx, "feature^2")
	require.NoError(t, err, "merge commit should have a second parent")
	assert.Equal(t, newBaseHash, secondParent)

	firstParent, err := repo.PeelToCommit(ctx, "feature^1")
	require.NoError(t, err)
	assert.Equal(t, oldFeatureHash, firstParent)

	assert.True(t, repo.IsAncestor(ctx, newBaseHash, newFeatureHash),
		"feature should contain newbase after the merge")

	// State reflects the new base and base hash.
	updated, err := svc.LookupBranch(ctx, "feature")
	require.NoError(t, err)
	assert.Equal(t, "newbase", updated.Base)
	assert.Equal(t, newBaseHash, updated.BaseHash)
}

func TestService_BranchOnto_merge_alreadyContainedNoOp(t *testing.T) {
	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		git init
		git config user.name Test
		git config user.email test@example.com
		git config commit.gpgSign false
		git commit --allow-empty -m 'Initial commit'

		# feature is stacked on base, which is on main:
		#
		#   main -- B1 (base) -- F1 (feature)
		git checkout -b base
		git add base.txt
		git commit -m 'Add base file'

		git checkout -b feature
		git add feature.txt
		git commit -m 'Add feature file'

		-- base.txt --
		base
		-- feature.txt --
		feature
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
	baseHash, err := repo.PeelToCommit(ctx, "base")
	require.NoError(t, err)
	featureHash, err := repo.PeelToCommit(ctx, "feature")
	require.NoError(t, err)

	store := NewMemoryStore(t)
	tx := store.BeginBranchTx()
	require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
		Name:     "base",
		Base:     "main",
		BaseHash: mainHash,
	}))
	require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
		Name:     "feature",
		Base:     "base",
		BaseHash: baseHash,
	}))
	require.NoError(t, tx.Commit(ctx, "setup"))

	svc := NewTestService(repo, wt, store, nil, silogtest.New(t),
		&ServiceOptions{RestackMethod: RestackMethodMerge})

	// Moving feature onto main is a no-op for the merge: main is already
	// an ancestor of feature, so "git merge" reports "Already up to date"
	// and creates no merge commit, but state still retargets.
	require.NoError(t, svc.BranchOnto(ctx, &BranchOntoRequest{
		Branch: "feature",
		Onto:   "main",
	}))

	newFeatureHash, err := repo.PeelToCommit(ctx, "feature")
	require.NoError(t, err)
	assert.Equal(t, featureHash, newFeatureHash,
		"feature head should be unchanged after a no-op merge")

	// No merge commit was created: feature has no second parent.
	_, err = repo.PeelToCommit(ctx, "feature^2")
	require.Error(t, err, "no-op merge should not create a merge commit")

	updated, err := svc.LookupBranch(ctx, "feature")
	require.NoError(t, err)
	assert.Equal(t, "main", updated.Base)
	assert.Equal(t, mainHash, updated.BaseHash)
}

func TestService_BranchOnto_merge_conflict(t *testing.T) {
	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		git init
		git config user.name Test
		git config user.email test@example.com
		git config commit.gpgSign false
		git commit --allow-empty -m 'Initial commit'

		# newbase and feature edit the same file with different
		# contents, so merging newbase into feature conflicts.
		#
		#   main -- B1 (newbase, edits shared.txt)
		#    \
		#     F1 (feature, edits shared.txt)
		git checkout -b newbase
		cp $WORK/extra/newbase-shared.txt shared.txt
		git add shared.txt
		git commit -m 'Edit shared file on newbase'

		git checkout main
		git checkout -b feature
		cp $WORK/extra/feature-shared.txt shared.txt
		git add shared.txt
		git commit -m 'Edit shared file on feature'

		-- extra/newbase-shared.txt --
		newbase change
		-- extra/feature-shared.txt --
		feature change
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

	store := NewMemoryStore(t)
	tx := store.BeginBranchTx()
	require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
		Name:     "newbase",
		Base:     "main",
		BaseHash: mainHash,
	}))
	require.NoError(t, tx.Upsert(ctx, state.UpsertRequest{
		Name:     "feature",
		Base:     "main",
		BaseHash: mainHash,
	}))
	require.NoError(t, tx.Commit(ctx, "setup"))

	svc := NewTestService(repo, wt, store, nil, silogtest.New(t),
		&ServiceOptions{RestackMethod: RestackMethodMerge})

	err = svc.BranchOnto(ctx, &BranchOntoRequest{
		Branch: "feature",
		Onto:   "newbase",
	})
	require.Error(t, err)

	var mergeErr *git.MergeInterruptError
	require.ErrorAs(t, err, &mergeErr)
	assert.Equal(t, "feature", mergeErr.State.Branch)

	// Clean up the in-progress merge so the worktree is restored.
	assert.NoError(t, wt.MergeAbort(ctx))
}
