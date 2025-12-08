package gitlab_test

import (
	"context"
	"crypto/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gogitlab "gitlab.com/gitlab-org/api/client-go"
	"go.abhg.dev/gs/internal/fixturetest"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/forge/gitlab"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

// This file tests basic, end-to-end interactions with the GitLab API
// using recorded fixtures.

var _fixtures = fixturetest.Config{Update: forgetest.Update}

// To avoid looking this up for every test that needs the repo ID,
// we'll just hardcode it here.
var (
	_testRepoID = gogitlab.Ptr(int64(64779801))
)

// TODO: delete newRecorder when tests have been migrated to forgetest.
func newRecorder(t *testing.T, name string) *recorder.Recorder {
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("To update the test fixtures, run:")
			t.Logf("    GITLAB_TOKEN=$token go test -update -run '^%s$'", t.Name())
		}
	})

	return forgetest.NewHTTPRecorder(t, name)
}

func newGitLabClient(
	httpClient *http.Client,
) *gitlab.Client {
	tok, exists := os.LookupEnv("GITLAB_TOKEN")
	var token string
	if !exists {
		token = "token"
	} else {
		token = tok
	}
	client, _ := gogitlab.NewClient(token, gogitlab.WithHTTPClient(httpClient))
	return &gitlab.Client{
		MergeRequests:    client.MergeRequests,
		Notes:            client.Notes,
		ProjectTemplates: client.ProjectTemplates,
		Projects:         client.Projects,
		Users:            client.Users,
	}
}

func TestIntegration_Repository(t *testing.T) {
	ctx := t.Context()
	rec := newRecorder(t, t.Name())
	ghc := newGitLabClient(rec.GetDefaultClient())
	_, err := gitlab.NewRepository(ctx, new(gitlab.Forge), "abg", "test-repo", silogtest.New(t), ghc, nil)
	require.NoError(t, err)
}

func TestIntegration_Repository_NewChangeMetadata(t *testing.T) {
	ctx := t.Context()

	rec := newRecorder(t, t.Name())
	ghc := newGitLabClient(rec.GetDefaultClient())
	repo, err := gitlab.NewRepository(
		ctx,
		new(gitlab.Forge), "abg", "test-repo",
		silogtest.New(t), ghc,
		&gitlab.RepositoryOptions{RepositoryID: _testRepoID},
	)
	require.NoError(t, err)

	t.Run("valid", func(t *testing.T) {
		ctx := t.Context()
		md, err := repo.NewChangeMetadata(ctx, &gitlab.MR{Number: 3})
		require.NoError(t, err)

		assert.Equal(t, &gitlab.MR{
			Number: 3,
		}, md.ChangeID())
		assert.Equal(t, "gitlab", md.ForgeID())
	})

	t.Run("invalid", func(t *testing.T) {
		ctx := t.Context()
		_, err := repo.NewChangeMetadata(ctx, &gitlab.MR{Number: 10000})
		require.NoError(t, err)
	})
}

func TestIntegration(t *testing.T) {
	t.Cleanup(func() {
		if t.Failed() && !forgetest.Update() {
			t.Logf("To update the test fixtures, run:")
			t.Logf("    GITLAB_TOKEN=$token go test -update -run '^%s$'", t.Name())
		}
	})

	gitlabForge := gitlab.Forge{
		Log: silogtest.New(t),
	}

	forgetest.RunIntegration(t, forgetest.IntegrationConfig{
		RemoteURL: "git@gitlab.com:abg/test-repo.git",
		Forge:     &gitlabForge,
		OpenRepository: func(t *testing.T, httpClient *http.Client) forge.Repository {
			ghc := newGitLabClient(httpClient)
			repo, err := gitlab.NewRepository(
				t.Context(), &gitlabForge, "abg", "test-repo",
				silogtest.New(t), ghc, &gitlab.RepositoryOptions{RepositoryID: _testRepoID},
			)
			require.NoError(t, err)
			return repo
		},
		MergeChange: func(t *testing.T, repo forge.Repository, change forge.ChangeID) {
			require.NoError(t, gitlab.MergeChange(t.Context(), repo.(*gitlab.Repository), change.(*gitlab.MR)))
		},
		CloseChange: func(t *testing.T, repo forge.Repository, change forge.ChangeID) {
			require.NoError(t, gitlab.CloseChange(t.Context(), repo.(*gitlab.Repository), change.(*gitlab.MR)))
		},
		SetCommentsPageSize:   gitlab.SetListChangeCommentsPageSize,
		BaseBranchMayBeAbsent: true,
		Reviewers:             []string{"abg"},
		Assignees:             []string{"abg"},
	})
}

func TestIntegration_Repository_notFoundError(t *testing.T) {
	ctx := t.Context()
	rec := newRecorder(t, t.Name())
	client := rec.GetDefaultClient()
	ghc := newGitLabClient(client)
	_, err := gitlab.NewRepository(ctx, new(gitlab.Forge), "abg", "does-not-exist-repo", silogtest.New(t), ghc, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "404 Not Found")
}

func TestIntegration_Repository_SubmitChange_removeSourceBranch(t *testing.T) {
	ctx := t.Context()
	branchFixture := fixturetest.New(_fixtures, "branch", func() string {
		return randomString(8)
	})

	branchName := branchFixture.Get(t)
	t.Logf("Creating branch: %s", branchName)

	var (
		gitRepo *git.Repository // only when _update is true
		gitWork *git.Worktree
	)
	if forgetest.Update() {
		t.Setenv("GIT_AUTHOR_EMAIL", "bot@example.com")
		t.Setenv("GIT_AUTHOR_NAME", "gs-test[bot]")
		t.Setenv("GIT_COMMITTER_EMAIL", "bot@example.com")
		t.Setenv("GIT_COMMITTER_NAME", "gs-test[bot]")

		output := t.Output()

		t.Logf("Cloning test-repo...")
		repoDir := t.TempDir()
		cmd := exec.Command("git", "clone", "git@gitlab.com:abg/test-repo.git", repoDir)
		cmd.Stdout = output
		cmd.Stderr = output
		require.NoError(t, cmd.Run(), "failed to clone test-repo")

		var err error
		gitWork, err = git.OpenWorktree(ctx, repoDir, git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err, "failed to open git repo")
		gitRepo = gitWork.Repository()

		require.NoError(t, gitRepo.CreateBranch(ctx, git.CreateBranchRequest{
			Name: branchName,
		}), "could not create branch: %s", branchName)
		require.NoError(t, gitWork.CheckoutBranch(ctx, branchName),
			"could not checkout branch: %s", branchName)
		require.NoError(t, os.WriteFile(
			filepath.Join(repoDir, branchName+".txt"),
			[]byte(randomString(32)),
			0o644,
		), "could not write file to branch")

		cmd = exec.Command("git", "add", ".")
		cmd.Dir = repoDir
		cmd.Stdout = output
		cmd.Stderr = output
		require.NoError(t, cmd.Run(), "git add failed")
		require.NoError(t, gitWork.Commit(ctx, git.CommitRequest{
			Message: "commit from test",
		}), "could not commit changes")

		t.Logf("Pushing to origin")
		require.NoError(t,
			gitWork.Push(ctx, git.PushOptions{
				Remote:  "origin",
				Refspec: git.Refspec(branchName),
			}), "error pushing branch")

		t.Cleanup(func() {
			ctx := context.WithoutCancel(t.Context())
			t.Logf("Deleting remote branch: %s", branchName)
			assert.NoError(t,
				gitWork.Push(ctx, git.PushOptions{
					Remote:  "origin",
					Refspec: git.Refspec(":" + branchName),
				}), "error deleting branch")
		})
	}

	rec := newRecorder(t, t.Name())
	ghc := newGitLabClient(rec.GetDefaultClient())
	repo, err := gitlab.NewRepository(
		ctx, new(gitlab.Forge), "abg", "test-repo", silogtest.New(t), ghc,
		&gitlab.RepositoryOptions{
			RepositoryID:              _testRepoID,
			RemoveSourceBranchOnMerge: true,
		},
	)
	require.NoError(t, err)

	change, err := repo.SubmitChange(ctx, forge.SubmitChangeRequest{
		Subject: branchName,
		Body:    "Test MR with RemoveSourceBranch option",
		Base:    "main",
		Head:    branchName,
	})
	require.NoError(t, err, "error creating MR")

	mrID := change.ID.(*gitlab.MR)
	mr, _, err := ghc.MergeRequests.GetMergeRequest(
		*_testRepoID, mrID.Number, nil,
		gogitlab.WithContext(ctx),
	)
	require.NoError(t, err, "error fetching created MR")
	assert.True(t, mr.ForceRemoveSourceBranch,
		"RemoveSourceBranch should be true on created MR")
}

const _alnum = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// randomString generates a random alphanumeric string of length n.
func randomString(n int) string {
	b := make([]byte, n)
	for i := range b {
		var buf [1]byte
		_, _ = rand.Read(buf[:])
		idx := int(buf[0]) % len(_alnum)
		b[i] = _alnum[idx]
	}
	return string(b)
}
