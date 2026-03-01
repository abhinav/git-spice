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
	"go.abhg.dev/gs/internal/httptest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

// This file tests basic, end-to-end interactions with the GitLab API
// using recorded fixtures.

var _fixtures = fixturetest.Config{Update: forgetest.Update}

// testConfig returns the GitLab test configuration and sanitizers for VCR fixtures.
// In update mode, loads from testconfig.yaml.
// In replay mode, returns canonical placeholders.
func testConfig(t *testing.T) (cfg forgetest.ForgeConfig, sanitizers []httptest.Sanitizer) {
	config := forgetest.Config(t)
	cfg = config.GitLab
	canonical := forgetest.CanonicalGitLabConfig()
	sanitizers = forgetest.ConfigSanitizers(cfg, canonical)
	return cfg, sanitizers
}

// TODO: delete newRecorder when tests have been migrated to forgetest.
func newRecorder(
	t *testing.T,
	name string,
	sanitizers []httptest.Sanitizer,
) *recorder.Recorder {
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("To update the test fixtures, run:")
			t.Logf("    GITLAB_TEST_OWNER=$owner GITLAB_TEST_REPO=$repo GITLAB_TOKEN=$token go test -update -run '^%s$'", t.Name())
		}
	})

	return forgetest.NewHTTPRecorder(t, name, sanitizers)
}

func newGitLabClient(
	t *testing.T,
	httpClient *http.Client,
) *gitlab.Client {
	// GitLab API requires a Personal Access Token with 'api' scope.
	// GCM tokens don't have sufficient scope, so GITLAB_TOKEN env var is required.
	token := forgetest.Token(t, "https://gitlab.com", "GITLAB_TOKEN")
	client, err := gogitlab.NewClient(token, gogitlab.WithHTTPClient(httpClient))
	require.NoError(t, err)
	return &gitlab.Client{
		MergeRequests:    client.MergeRequests,
		Notes:            client.Notes,
		ProjectTemplates: client.ProjectTemplates,
		Projects:         client.Projects,
		Users:            client.Users,
	}
}

func TestIntegration_Repository(t *testing.T) {
	cfg, sanitizers := testConfig(t)
	ctx := t.Context()
	rec := newRecorder(t, t.Name(), sanitizers)
	ghc := newGitLabClient(t, rec.GetDefaultClient())
	_, err := gitlab.NewRepository(ctx, new(gitlab.Forge), cfg.Owner, cfg.Repo, silogtest.New(t), ghc, nil)
	require.NoError(t, err)
}

func TestIntegration(t *testing.T) {
	cfg, sanitizers := testConfig(t)
	remoteURL := "https://gitlab.com/" + cfg.Owner + "/" + cfg.Repo

	t.Cleanup(func() {
		if t.Failed() && !forgetest.Update() {
			t.Logf("To update the test fixtures, run:")
			t.Logf("    Configure testconfig.yaml and run: GITLAB_TOKEN=$token go test -update -run '^%s$'", t.Name())
		}
	})

	gitlabForge := gitlab.Forge{
		Log: silogtest.New(t),
	}

	forgetest.RunIntegration(t, forgetest.IntegrationConfig{
		RemoteURL:  remoteURL,
		Forge:      &gitlabForge,
		Sanitizers: sanitizers,
		OpenRepository: func(t *testing.T, httpClient *http.Client) forge.Repository {
			ghc := newGitLabClient(t, httpClient)
			newRepo, err := gitlab.NewRepository(
				t.Context(), &gitlabForge, cfg.Owner, cfg.Repo,
				silogtest.New(t), ghc, nil,
			)
			require.NoError(t, err)
			return newRepo
		},
		MergeChange: func(t *testing.T, repo forge.Repository, change forge.ChangeID) {
			require.NoError(t, gitlab.MergeChange(t.Context(), repo.(*gitlab.Repository), change.(*gitlab.MR)))
		},
		CloseChange: func(t *testing.T, repo forge.Repository, change forge.ChangeID) {
			require.NoError(t, gitlab.CloseChange(t.Context(), repo.(*gitlab.Repository), change.(*gitlab.MR)))
		},
		SetCommentsPageSize:   gitlab.SetListChangeCommentsPageSize,
		BaseBranchMayBeAbsent: true,
		SkipMerge:             true, // Merge requires MR approval settings to be disabled
		Reviewers:             []string{cfg.Reviewer},
		Assignees:             []string{cfg.Assignee},
	})
}

func TestIntegration_Repository_notFoundError(t *testing.T) {
	cfg, sanitizers := testConfig(t)
	ctx := t.Context()
	rec := newRecorder(t, t.Name(), sanitizers)
	client := rec.GetDefaultClient()
	ghc := newGitLabClient(t, client)
	_, err := gitlab.NewRepository(ctx, new(gitlab.Forge), cfg.Owner, "does-not-exist-repo", silogtest.New(t), ghc, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "404 Not Found")
}

func TestIntegration_Repository_SubmitChange_removeSourceBranch(t *testing.T) {
	cfg, sanitizers := testConfig(t)
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
		cmd := exec.Command("git", "clone", "https://gitlab.com/"+cfg.Owner+"/"+cfg.Repo+".git", repoDir)
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

	rec := newRecorder(t, t.Name(), sanitizers)
	ghc := newGitLabClient(t, rec.GetDefaultClient())
	repo, err := gitlab.NewRepository(
		ctx, new(gitlab.Forge), cfg.Owner, cfg.Repo, silogtest.New(t), ghc,
		&gitlab.RepositoryOptions{
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
	projectPath := cfg.Owner + "/" + cfg.Repo
	mr, _, err := ghc.MergeRequests.GetMergeRequest(
		projectPath, mrID.Number, nil,
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
