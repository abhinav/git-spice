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

	svc := NewTestService(repo, wt, store, nil, silogtest.New(t))

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
