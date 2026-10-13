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
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestStackRepository_MergeRange_merge(t *testing.T) {
	fixture := newMergeRangeFixture(t)

	operation, err := fixture.plan().Merge(t.Context(), fixture.request)
	require.NoError(t, err)
	assert.Nil(t, operation)
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
	assert.Nil(t, operation)
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
}

func TestStackRepository_MergeRange_validationFailureIsAtomic(t *testing.T) {
	fixture := newMergeRangeFixture(t)
	fixture.request.Changes[1].HeadHash = git.Hash(strings.Repeat("1", 40))

	_, err := fixture.plan().Merge(t.Context(), fixture.request)
	assert.ErrorContains(t, err, "head hash mismatch")
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
				Number:   int(fixture.request.Changes[0].Change.(ChangeID)),
				Base:     fixture.request.Changes[0].Base,
				Head:     fixture.request.Changes[0].Head,
				HeadHash: fixture.request.Changes[0].HeadHash.String(),
			},
			{
				Number:   int(fixture.request.Changes[1].Change.(ChangeID)),
				Base:     fixture.request.Changes[1].Base,
				Head:     fixture.request.Changes[1].Head,
				HeadHash: fixture.request.Changes[1].HeadHash.String(),
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
	request  forge.MergeRangeRequest
	mainHash string
	topHash  git.Hash
}

func (f mergeRangeFixture) plan() *shamHubMergeRangePlan {
	changes := make([]forge.ChangeID, len(f.request.Changes))
	for i, change := range f.request.Changes {
		changes[i] = change.Change
	}
	return &shamHubMergeRangePlan{
		repository: f.repo,
		changes:    changes,
	}
}

func newMergeRangeFixture(t *testing.T) mergeRangeFixture {
	t.Helper()

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
		mainHash: mainHash.String(),
		topHash:  topHash,
		request: forge.MergeRangeRequest{Changes: []forge.MergeRangeChange{
			{
				Change:   bottom.ID,
				Base:     "main",
				Head:     "bottom",
				HeadHash: bottomHash,
			},
			{
				Change:   top.ID,
				Base:     "bottom",
				Head:     "top",
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
		{State: forge.ChangeMerged, HeadHash: fixture.request.Changes[0].HeadHash},
		{State: forge.ChangeMerged, HeadHash: fixture.request.Changes[1].HeadHash},
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
