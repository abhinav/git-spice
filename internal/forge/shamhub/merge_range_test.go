package shamhub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestStackRepository_MergeRange_merge(t *testing.T) {
	fixture := newMergeRangeFixture(t)
	request := fixture.request
	request.Heads = []forge.ChangeHead{fixture.request.Heads[1], fixture.request.Heads[0]}

	operation, err := fixture.plan().Merge(t.Context(), request)
	require.NoError(t, err)
	require.NotNil(t, operation)
	status, err := operation.Status(t.Context())
	require.NoError(t, err)
	assert.Equal(t, forge.MergeOperationPending, status)
	status, err = operation.Status(t.Context())
	require.NoError(t, err)
	assert.Equal(t, forge.MergeOperationAccepted, status)
	status, err = operation.Status(t.Context())
	require.NoError(t, err)
	assert.Equal(t, forge.MergeOperationAccepted, status)
	assertMergeRangeCompleted(t, fixture)

	mainHash := resolveTestBranch(t, fixture.sh, "main")
	commit, err := fixture.sh.gitCmd(
		t.Context(), "alice", "example", "cat-file", "-p", mainHash,
	).OutputChomp()
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(commit, "\nparent "))
	assert.Equal(t,
		resolveTestTree(t, fixture.sh, fixture.topHash.String()),
		resolveTestTree(t, fixture.sh, mainHash),
	)
}

func TestStackRepository_MergeRange_squash(t *testing.T) {
	fixture := newMergeRangeFixture(t)
	fixture.request.Method = forge.MergeMethodSquash

	operation, err := fixture.plan().Merge(t.Context(), fixture.request)
	require.NoError(t, err)
	require.NotNil(t, operation)
	assertMergeRangeCompleted(t, fixture)

	mainHash := resolveTestBranch(t, fixture.sh, "main")
	count, err := fixture.sh.gitCmd(
		t.Context(), "alice", "example", "rev-list", "--count",
		fixture.mainHash+".."+mainHash,
	).OutputChomp()
	require.NoError(t, err)
	assert.Equal(t, "2", count)
	assert.Equal(t,
		resolveTestTree(t, fixture.sh, fixture.topHash.String()),
		resolveTestTree(t, fixture.sh, mainHash),
	)
	assert.Equal(t,
		resolveTestTree(t, fixture.sh, fixture.request.Heads[0].HeadHash.String()),
		resolveTestTree(t, fixture.sh, mainHash+"^"),
	)
}

func TestStackRepository_MergeRange_squashUpstackRevert(t *testing.T) {
	fixture := newMergeRangeFixture(t)
	fixture.request.Method = forge.MergeMethodSquash

	// The top PR reverts all changes introduced by the bottom PR.
	// Its squash commit must restore the original tree, not retain the bottom.
	rootTree := resolveTestTree(t, fixture.sh, fixture.mainHash)
	topHash, err := fixture.sh.commitRangeTree(
		t.Context(), "alice", "example", rootTree,
		[]string{fixture.topHash.String()}, "Revert the stack", fixture.topHash.String(),
	)
	require.NoError(t, err)
	require.NoError(t, fixture.sh.gitCmd(
		t.Context(), "alice", "example", "update-ref", "refs/heads/top", topHash,
	).Run())
	fixture.topHash = git.Hash(topHash)
	fixture.request.Heads[1].HeadHash = fixture.topHash

	operation, err := fixture.plan().Merge(t.Context(), fixture.request)
	require.NoError(t, err)
	require.NotNil(t, operation)
	assertMergeRangeCompleted(t, fixture)

	mainHash := resolveTestBranch(t, fixture.sh, "main")
	assert.Equal(t, rootTree, resolveTestTree(t, fixture.sh, mainHash))
	assert.Equal(t,
		resolveTestTree(t, fixture.sh, fixture.request.Heads[0].HeadHash.String()),
		resolveTestTree(t, fixture.sh, mainHash+"^"),
	)
}

func TestStackRepository_MergeRange_validationFailureIsAtomic(t *testing.T) {
	fixture := newMergeRangeFixture(t)
	fixture.request.Heads[1].HeadHash = git.Hash(strings.Repeat("1", 40))

	_, err := fixture.plan().Merge(t.Context(), fixture.request)
	assert.ErrorContains(t, err, "head hash mismatch")
	assertMergeRangeUnchanged(t, fixture)
}

func TestStackRepository_MergeRange_misalignedBases(t *testing.T) {
	fixture := newMergeRangeFixture(t)
	fixture.sh.changes[1].Base.Name = "main"

	_, err := fixture.plan().Merge(t.Context(), fixture.request)
	assert.ErrorContains(t, err, `base branch is "main", expected prior head "bottom"`)
	assertMergeRangeUnchanged(t, fixture)
}

func TestStackRepository_MergeRange_objectConstructionFailureIsAtomic(
	t *testing.T,
) {
	fixture := newMergeRangeFixture(t)
	fixture.sh.gitExe = rejectGitSubcommand(t, fixture.sh.gitExe, "commit-tree")

	_, err := fixture.plan().Merge(t.Context(), fixture.request)
	assert.ErrorContains(t, err, "create merge commit")
	assertMergeRangeUnchanged(t, fixture)
}

func TestStackRepository_MergeRange_refUpdateFailureIsAtomic(t *testing.T) {
	fixture := newMergeRangeFixture(t)
	fixture.sh.gitExe = rejectGitSubcommand(t, fixture.sh.gitExe, "update-ref")

	_, err := fixture.plan().Merge(t.Context(), fixture.request)
	assert.ErrorContains(t, err, "update root ref")
	assertMergeRangeUnchanged(t, fixture)
}

func TestShamHub_mergeRange_canceledRequestIsAtomic(t *testing.T) {
	fixture := newMergeRangeFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := fixture.sh.handleMergeRange(ctx, &mergeRangeRequest{
		Owner: "alice",
		Repo:  "example",
		Changes: []mergeRangeChange{
			{
				Number:   int(fixture.request.Heads[0].Change.(ChangeID)),
				HeadHash: fixture.request.Heads[0].HeadHash.String(),
			},
			{
				Number:   int(fixture.request.Heads[1].Change.(ChangeID)),
				HeadHash: fixture.request.Heads[1].HeadHash.String(),
			},
		},
		MergeMethod: string(MergeMethodMerge),
	})
	require.ErrorIs(t, err, context.Canceled)
	assertMergeRangeUnchanged(t, fixture)
}

type mergeRangeFixture struct {
	sh       *ShamHub
	repo     *stackRepository
	changes  []forge.ChangeID
	request  forge.MergeRangeRequest
	mainHash string
	topHash  git.Hash
}

func (f mergeRangeFixture) plan() *shamHubMergeRangePlan {
	return &shamHubMergeRangePlan{
		repository: f.repo,
		changes:    f.changes,
	}
}

func newMergeRangeFixture(t *testing.T) mergeRangeFixture {
	t.Helper()
	gittest.SkipUnlessVersionAtLeast(t, gittest.Version{Major: 2, Minor: 40})

	sh, baseRepo := newMergeabilityTestRepository(t)
	repo := &stackRepository{forgeRepository: baseRepo}
	workDir := t.TempDir()
	worktree, err := git.Clone(
		t.Context(),
		sh.RepoURL("alice", "example"),
		workDir,
		git.CloneOptions{Log: silogtest.New(t)},
	)
	require.NoError(t, err)

	writeCommitAndPush(t, worktree, workDir, "base.txt", "base\n", "Base", "main")
	mainHash, err := worktree.Head(t.Context())
	require.NoError(t, err)

	require.NoError(t, worktree.Repository().CreateBranch(
		t.Context(), git.CreateBranchRequest{Name: "bottom", Head: "HEAD"},
	))
	require.NoError(t, worktree.CheckoutBranch(t.Context(), "bottom"))
	writeCommitAndPush(t, worktree, workDir,
		"bottom.txt", "bottom\n", "Bottom", "bottom")
	bottomHash, err := worktree.Head(t.Context())
	require.NoError(t, err)

	require.NoError(t, worktree.Repository().CreateBranch(
		t.Context(), git.CreateBranchRequest{Name: "top", Head: "HEAD"},
	))
	require.NoError(t, worktree.CheckoutBranch(t.Context(), "top"))
	writeCommitAndPush(t, worktree, workDir,
		"top.txt", "top\n", "Top", "top")
	topHash, err := worktree.Head(t.Context())
	require.NoError(t, err)

	bottom, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Bottom change",
		Base:    "main",
		Head:    "bottom",
	})
	require.NoError(t, err)
	top, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Top change",
		Base:    "bottom",
		Head:    "top",
	})
	require.NoError(t, err)

	return mergeRangeFixture{
		sh:       sh,
		repo:     repo,
		changes:  []forge.ChangeID{bottom.ID, top.ID},
		mainHash: mainHash.String(),
		topHash:  topHash,
		request: forge.MergeRangeRequest{Heads: []forge.ChangeHead{
			{
				Change:   bottom.ID,
				HeadHash: bottomHash,
			},
			{
				Change:   top.ID,
				HeadHash: topHash,
			},
		}},
	}
}

func writeCommitAndPush(
	t *testing.T,
	worktree *git.Worktree,
	workDir string,
	filename string,
	contents string,
	message string,
	branch string,
) {
	t.Helper()

	require.NoError(t, os.WriteFile(
		filepath.Join(workDir, filename), []byte(contents), 0o644,
	))
	gitAdd(t, workDir, filename)
	require.NoError(t, worktree.Commit(
		t.Context(), git.CommitRequest{Message: message},
	))
	require.NoError(t, worktree.Push(t.Context(), git.PushOptions{
		Remote:  "origin",
		Refspec: git.Refspec(branch + ":" + branch),
	}))
}

func rejectGitSubcommand(t *testing.T, realGit, rejected string) string {
	t.Helper()

	gitWrapper := filepath.Join(t.TempDir(), "git")
	require.NoError(t, os.WriteFile(
		gitWrapper,
		fmt.Appendf(nil, `#!/bin/sh
for arg in "$@"; do
	if [ "$arg" = "%s" ]; then
		exit 1
	fi
done
exec "%s" "$@"
`, rejected, realGit),
		0o755,
	))
	return gitWrapper
}

func assertMergeRangeCompleted(t *testing.T, fixture mergeRangeFixture) {
	t.Helper()

	statuses, err := fixture.repo.ChangeStatuses(
		t.Context(), []forge.ChangeID{ChangeID(1), ChangeID(2)},
	)
	require.NoError(t, err)
	assert.Equal(t, []forge.ChangeStatus{
		{State: forge.ChangeMerged, HeadHash: fixture.request.Heads[0].HeadHash},
		{State: forge.ChangeMerged, HeadHash: fixture.request.Heads[1].HeadHash},
	}, statuses)
	assert.NotEqual(t, fixture.mainHash, resolveTestBranch(t, fixture.sh, "main"))
}

func assertMergeRangeUnchanged(t *testing.T, fixture mergeRangeFixture) {
	t.Helper()

	assert.Equal(t, fixture.mainHash, resolveTestBranch(t, fixture.sh, "main"))
	statuses, err := fixture.repo.ChangeStatuses(
		t.Context(), []forge.ChangeID{ChangeID(1), ChangeID(2)},
	)
	require.NoError(t, err)
	assert.Equal(t, forge.ChangeOpen, statuses[0].State)
	assert.Equal(t, forge.ChangeOpen, statuses[1].State)
}

func resolveTestBranch(t *testing.T, sh *ShamHub, branch string) string {
	t.Helper()

	hash, err := sh.gitCmd(
		t.Context(), "alice", "example", "rev-parse", "refs/heads/"+branch+"^{commit}",
	).OutputChomp()
	require.NoError(t, err)
	return hash
}

func resolveTestTree(t *testing.T, sh *ShamHub, commit string) string {
	t.Helper()

	tree, err := sh.gitCmd(
		t.Context(), "alice", "example", "rev-parse", commit+"^{tree}",
	).OutputChomp()
	require.NoError(t, err)
	return tree
}
