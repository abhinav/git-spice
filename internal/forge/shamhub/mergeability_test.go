package shamhub

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestForgeRepository_ChangeMergeability_configured(t *testing.T) {
	sh, repo := newMergeabilityTestRepository(t)
	seedMergeabilityChange(sh)

	require.NoError(t, sh.SetChangeMergeability(SetChangeMergeabilityRequest{
		Owner:  "alice",
		Repo:   "example",
		Number: 1,
		Mergeability: forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonPolicy,
		},
	}))

	got, err := repo.ChangeMergeability(t.Context(), ChangeID(1))
	require.NoError(t, err)
	assert.Equal(t, forge.ChangeMergeability{
		State:  forge.ChangeMergeabilityBlocked,
		Reason: forge.ChangeMergeabilityReasonPolicy,
	}, got)
}

func TestForgeRepository_setChangeMergeability(t *testing.T) {
	sh, repo := newMergeabilityTestRepository(t)
	seedMergeabilityChange(sh)

	require.NoError(t, repo.setChangeMergeability(
		t.Context(),
		ChangeID(1),
		forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityWaiting,
			Reason: forge.ChangeMergeabilityReasonChecks,
		},
	))

	got, err := repo.ChangeMergeability(t.Context(), ChangeID(1))
	require.NoError(t, err)
	assert.Equal(t, forge.ChangeMergeability{
		State:  forge.ChangeMergeabilityWaiting,
		Reason: forge.ChangeMergeabilityReasonChecks,
	}, got)
}

func TestCLI_setMergeability(t *testing.T) {
	sh, err := New(Config{Log: silogtest.New(t)})
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, sh.Close())
	})
	seedMergeabilityChange(sh)

	getenv := func(key string) string {
		switch key {
		case "SHAMHUB_API_URL":
			return sh.APIURL()
		case "SHAMHUB_URL":
			return sh.GitURL()
		case "SHAMHUB_ADMIN_TOKEN":
			return sh.AdminToken()
		default:
			return ""
		}
	}
	err = runCLI(
		t.Context(),
		[]string{
			"set-mergeability",
			"-reason", "checks",
			"alice/example",
			"1",
			"waiting",
		},
		getenv,
		new(bytes.Buffer),
		new(bytes.Buffer),
	)
	require.NoError(t, err)

	got, err := sh.ChangeMergeability("alice", "example", 1)
	require.NoError(t, err)
	want, err := parseMergeability("waiting", "checks")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestShamHub_ChangeMergeability_conflicts(t *testing.T) {
	sh, repo := newMergeabilityTestRepository(t)

	workDir := t.TempDir()
	worktree, err := git.Clone(t.Context(), sh.RepoURL("alice", "example"), workDir, git.CloneOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(
		filepath.Join(workDir, "README.md"),
		[]byte("base\n"),
		0o644,
	))
	gitAdd(t, workDir, "README.md")
	require.NoError(t, worktree.Commit(t.Context(), git.CommitRequest{
		Message: "Initial commit",
	}))
	require.NoError(t, worktree.Push(t.Context(), git.PushOptions{
		Remote:  "origin",
		Refspec: "main:main",
	}))

	require.NoError(t, worktree.Repository().CreateBranch(t.Context(), git.CreateBranchRequest{
		Name: "feature",
		Head: "HEAD",
	}))
	require.NoError(t, worktree.CheckoutBranch(t.Context(), "feature"))
	require.NoError(t, os.WriteFile(
		filepath.Join(workDir, "README.md"),
		[]byte("feature\n"),
		0o644,
	))
	gitAdd(t, workDir, "README.md")
	require.NoError(t, worktree.Commit(t.Context(), git.CommitRequest{
		Message: "Feature change",
	}))
	require.NoError(t, worktree.Push(t.Context(), git.PushOptions{
		Remote:  "origin",
		Refspec: "feature:feature",
	}))

	require.NoError(t, worktree.CheckoutBranch(t.Context(), "main"))
	require.NoError(t, os.WriteFile(
		filepath.Join(workDir, "README.md"),
		[]byte("main\n"),
		0o644,
	))
	gitAdd(t, workDir, "README.md")
	require.NoError(t, worktree.Commit(t.Context(), git.CommitRequest{
		Message: "Main change",
	}))
	require.NoError(t, worktree.Push(t.Context(), git.PushOptions{
		Remote:  "origin",
		Refspec: "main:main",
	}))

	submitMergeabilityChange(t, repo)

	got, err := sh.ChangeMergeability("alice", "example", 1)
	require.NoError(t, err)
	assert.Equal(t, forge.ChangeMergeability{
		State:  forge.ChangeMergeabilityBlocked,
		Reason: forge.ChangeMergeabilityReasonConflicts,
	}, got)
}

func TestShamHub_ChangeMergeability_ready(t *testing.T) {
	sh, repo := newMergeabilityTestRepository(t)

	workDir := t.TempDir()
	worktree, err := git.Clone(t.Context(), sh.RepoURL("alice", "example"), workDir, git.CloneOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(
		filepath.Join(workDir, "README.md"),
		[]byte("base\n"),
		0o644,
	))
	gitAdd(t, workDir, "README.md")
	require.NoError(t, worktree.Commit(t.Context(), git.CommitRequest{
		Message: "Initial commit",
	}))
	require.NoError(t, worktree.Push(t.Context(), git.PushOptions{
		Remote:  "origin",
		Refspec: "main:main",
	}))

	require.NoError(t, worktree.Repository().CreateBranch(t.Context(), git.CreateBranchRequest{
		Name: "feature",
		Head: "HEAD",
	}))
	require.NoError(t, worktree.CheckoutBranch(t.Context(), "feature"))
	require.NoError(t, os.WriteFile(
		filepath.Join(workDir, "feature.txt"),
		[]byte("feature\n"),
		0o644,
	))
	gitAdd(t, workDir, "feature.txt")
	require.NoError(t, worktree.Commit(t.Context(), git.CommitRequest{
		Message: "Feature change",
	}))
	require.NoError(t, worktree.Push(t.Context(), git.PushOptions{
		Remote:  "origin",
		Refspec: "feature:feature",
	}))

	submitMergeabilityChange(t, repo)

	got, err := sh.ChangeMergeability("alice", "example", 1)
	require.NoError(t, err)
	assert.Equal(t, forge.ChangeMergeability{
		State: forge.ChangeMergeabilityReady,
	}, got)
}

func TestShamHub_ChangeMergeability_forkedHeadForcePush(t *testing.T) {
	sh, repo := newMergeabilityTestRepository(t)

	baseDir := t.TempDir()
	baseWorktree, err := git.Clone(
		t.Context(),
		sh.RepoURL("alice", "example"),
		baseDir,
		git.CloneOptions{Log: silogtest.New(t)},
	)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(
		filepath.Join(baseDir, "README.md"),
		[]byte("base\n"),
		0o644,
	))
	gitAdd(t, baseDir, "README.md")
	require.NoError(t, baseWorktree.Commit(t.Context(), git.CommitRequest{
		Message: "Initial commit",
	}))
	require.NoError(t, baseWorktree.Push(t.Context(), git.PushOptions{
		Remote:  "origin",
		Refspec: "main:main",
	}))

	forkURL, err := sh.ForkRepository("alice", "example", "bob")
	require.NoError(t, err)

	forkDir := t.TempDir()
	forkWorktree, err := git.Clone(
		t.Context(),
		forkURL,
		forkDir,
		git.CloneOptions{Log: silogtest.New(t)},
	)
	require.NoError(t, err)

	require.NoError(t, forkWorktree.Repository().CreateBranch(
		t.Context(),
		git.CreateBranchRequest{
			Name: "feature",
			Head: "HEAD",
		},
	))
	require.NoError(t, forkWorktree.CheckoutBranch(t.Context(), "feature"))
	require.NoError(t, os.WriteFile(
		filepath.Join(forkDir, "feature.txt"),
		[]byte("feature\n"),
		0o644,
	))
	gitAdd(t, forkDir, "feature.txt")
	require.NoError(t, forkWorktree.Commit(t.Context(), git.CommitRequest{
		Message: "Feature change",
	}))
	require.NoError(t, forkWorktree.Push(t.Context(), git.PushOptions{
		Remote:  "origin",
		Refspec: "feature:feature",
	}))

	result, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Fork feature",
		Base:    "main",
		Head:    "feature",
		PushRepository: &RepositoryID{
			url:   forkURL,
			owner: "bob",
			repo:  "example",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, ChangeID(1), result.ID)

	got, err := sh.ChangeMergeability("alice", "example", 1)
	require.NoError(t, err)
	assert.Equal(t, forge.ChangeMergeability{
		State: forge.ChangeMergeabilityReady,
	}, got)

	require.NoError(t, forkWorktree.CheckoutBranch(t.Context(), "main"))
	require.NoError(t, forkWorktree.Repository().CreateBranch(
		t.Context(),
		git.CreateBranchRequest{
			Name:  "feature",
			Head:  "HEAD",
			Force: true,
		},
	))
	require.NoError(t, forkWorktree.CheckoutBranch(t.Context(), "feature"))
	require.NoError(t, os.WriteFile(
		filepath.Join(forkDir, "other.txt"),
		[]byte("other\n"),
		0o644,
	))
	gitAdd(t, forkDir, "other.txt")
	require.NoError(t, forkWorktree.Commit(t.Context(), git.CommitRequest{
		Message: "Replace feature change",
	}))
	require.NoError(t, forkWorktree.Push(t.Context(), git.PushOptions{
		Remote:  "origin",
		Refspec: "feature:feature",
		Force:   true,
	}))

	got, err = sh.ChangeMergeability("alice", "example", 1)
	require.NoError(t, err)
	assert.Equal(t, forge.ChangeMergeability{
		State: forge.ChangeMergeabilityReady,
	}, got)
}

func TestShamHub_ChangeMergeability_deletedHeadErrors(t *testing.T) {
	sh, repo := newMergeabilityTestRepository(t)

	workDir := t.TempDir()
	worktree, err := git.Clone(t.Context(), sh.RepoURL("alice", "example"), workDir, git.CloneOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(
		filepath.Join(workDir, "README.md"),
		[]byte("base\n"),
		0o644,
	))
	gitAdd(t, workDir, "README.md")
	require.NoError(t, worktree.Commit(t.Context(), git.CommitRequest{
		Message: "Initial commit",
	}))
	require.NoError(t, worktree.Push(t.Context(), git.PushOptions{
		Remote:  "origin",
		Refspec: "main:main",
	}))

	require.NoError(t, worktree.Repository().CreateBranch(t.Context(), git.CreateBranchRequest{
		Name: "feature",
		Head: "HEAD",
	}))
	require.NoError(t, worktree.CheckoutBranch(t.Context(), "feature"))
	require.NoError(t, os.WriteFile(
		filepath.Join(workDir, "feature.txt"),
		[]byte("feature\n"),
		0o644,
	))
	gitAdd(t, workDir, "feature.txt")
	require.NoError(t, worktree.Commit(t.Context(), git.CommitRequest{
		Message: "Feature change",
	}))
	require.NoError(t, worktree.Push(t.Context(), git.PushOptions{
		Remote:  "origin",
		Refspec: "feature:feature",
	}))

	submitMergeabilityChange(t, repo)

	require.NoError(t, worktree.Push(t.Context(), git.PushOptions{
		Remote:  "origin",
		Refspec: ":feature",
	}))

	_, err = sh.ChangeMergeability("alice", "example", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "check mergeability")
}

func newMergeabilityTestRepository(t *testing.T) (*ShamHub, *forgeRepository) {
	t.Helper()

	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "commit.gpgsign")
	t.Setenv("GIT_CONFIG_VALUE_0", "false")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")

	sh, err := New(Config{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, sh.Close())
	})

	repoURL, err := sh.NewRepository("alice", "example")
	require.NoError(t, err)
	require.NoError(t, sh.RegisterUser("test-user"))
	token, err := sh.IssueToken("test-user")
	require.NoError(t, err)

	repository, err := newRepository(
		&Forge{
			Options: Options{
				URL:    sh.GitURL(),
				APIURL: sh.APIURL(),
			},
			Log: silog.Nop(),
		},
		&AuthenticationToken{tok: token},
		&RepositoryID{
			url:   repoURL,
			owner: "alice",
			repo:  "example",
		},
		http.DefaultClient,
	)
	require.NoError(t, err)
	repo := repository.(*forgeRepository)

	return sh, repo
}

func seedMergeabilityChange(sh *ShamHub) {
	sh.changes = append(sh.changes, shamChange{
		Number: 1,
		Base: &shamBranch{
			Owner: "alice",
			Repo:  "example",
			Name:  "main",
		},
		Head: &shamBranch{
			Owner: "alice",
			Repo:  "example",
			Name:  "feature",
		},
	})
}

func submitMergeabilityChange(t *testing.T, repo *forgeRepository) {
	t.Helper()

	result, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Feature change",
		Base:    "main",
		Head:    "feature",
	})
	require.NoError(t, err)
	assert.Equal(t, ChangeID(1), result.ID)
}
