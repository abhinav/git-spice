package gitlab_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gogitlab "gitlab.com/gitlab-org/api/client-go"
	"go.abhg.dev/gs/internal/fixturetest"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/gitlab"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/httptest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

// This file tests basic, end-to-end interactions with the GitLab API
// using recorded fixtures.

var (
	_update   = gitlab.UpdateFixtures
	_fixtures = fixturetest.Config{Update: gitlab.UpdateFixtures}
)

// To avoid looking this up for every test that needs the repo ID,
// we'll just hardcode it here.
var (
	_testRepoID = gogitlab.Ptr(64779801)
)

func newRecorder(t *testing.T, name string) *recorder.Recorder {
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("To update the test fixtures, run:")
			t.Logf("    GITLAB_TOKEN=$token go test -update -run '^%s$'", t.Name())
		}
	})

	return httptest.NewTransportRecorder(t, name, httptest.TransportRecorderOptions{
		Update: _update,
		WrapRealTransport: func(t testing.TB, transport http.RoundTripper) http.RoundTripper {
			gitlabToken := os.Getenv("GITLAB_TOKEN")
			require.NotEmpty(t, gitlabToken,
				"$GITLAB_TOKEN must be set in record mode")

			return transport
		},

		Matcher: func(r *http.Request, i cassette.Request) bool {
			if r.Body == nil || r.Body == http.NoBody {
				return r.Method == i.Method && r.URL.String() == i.URL
			}

			reqBody, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			assert.NoError(t, r.Body.Close())

			r.Body = io.NopCloser(bytes.NewBuffer(reqBody))
			return r.Method == i.Method &&
				r.URL.String() == i.URL &&
				string(reqBody) == i.Body
		},
	})
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

func TestIntegration_Repository_FindChangeByID(t *testing.T) {
	ctx := t.Context()
	rec := newRecorder(t, t.Name())
	ghc := newGitLabClient(rec.GetDefaultClient())
	repo, err := gitlab.NewRepository(
		ctx,
		new(gitlab.Forge),
		"abg", "test-repo",
		silogtest.New(t), ghc,
		&gitlab.RepositoryOptions{RepositoryID: _testRepoID},
	)
	require.NoError(t, err)

	t.Run("found", func(t *testing.T) {
		ctx := t.Context()
		change, err := repo.FindChangeByID(ctx, &gitlab.MR{Number: 2})
		require.NoError(t, err)

		assert.Equal(t, &forge.FindChangeItem{
			ID: &gitlab.MR{
				Number: 2,
			},
			URL:      "https://gitlab.com/abg/test-repo/-/merge_requests/2",
			Subject:  "foo",
			State:    forge.ChangeMerged,
			BaseName: "main",
			HeadHash: "12750c0228ffa8973637dfeee5b07e97e49c5fbe",
			Draft:    false,
		}, change)
	})

	t.Run("not-found", func(t *testing.T) {
		ctx := t.Context()
		_, err := repo.FindChangeByID(ctx, &gitlab.MR{Number: 999})
		require.Error(t, err)
		assert.ErrorContains(t, err, "404 Not Found")
	})
}

func TestIntegration_Repository_FindChangesByBranch(t *testing.T) {
	ctx := t.Context()
	rec := newRecorder(t, t.Name())
	ghc := newGitLabClient(rec.GetDefaultClient())
	repo, err := gitlab.NewRepository(
		ctx,
		new(gitlab.Forge), "abg", "test-repo", silogtest.New(t), ghc,
		&gitlab.RepositoryOptions{RepositoryID: _testRepoID},
	)
	require.NoError(t, err)

	t.Run("found", func(t *testing.T) {
		ctx := t.Context()
		changes, err := repo.FindChangesByBranch(ctx, "branch-name-test-TMmz0MkH", forge.FindChangesOptions{})
		require.NoError(t, err)
		assert.Equal(t, []*forge.FindChangeItem{
			{
				ID: &gitlab.MR{
					Number: 3,
				},
				URL:      "https://gitlab.com/abg/test-repo/-/merge_requests/3",
				Subject:  "branch name test",
				State:    forge.ChangeMerged,
				BaseName: "main",
				HeadHash: "8c33013dc1ff6e5bc86d5e5400a227aabb9a77f6",
				Draft:    false,
			},
		}, changes)
	})

	t.Run("not-found", func(t *testing.T) {
		ctx := t.Context()
		changes, err := repo.FindChangesByBranch(ctx, "does-not-exist", forge.FindChangesOptions{})
		require.NoError(t, err)
		assert.Empty(t, changes)
	})
}

func TestIntegration_Repository_ChangesStates(t *testing.T) {
	ctx := t.Context()
	rec := newRecorder(t, t.Name())
	ghc := newGitLabClient(rec.GetDefaultClient())
	repo, err := gitlab.NewRepository(
		ctx,
		new(gitlab.Forge),
		"abg", "test-repo", silogtest.New(t), ghc,
		&gitlab.RepositoryOptions{RepositoryID: _testRepoID},
	)
	require.NoError(t, err)

	states, err := repo.ChangesStates(ctx, []forge.ChangeID{
		&gitlab.MR{Number: 2},  // merged
		&gitlab.MR{Number: 4},  // open (not merged)
		&gitlab.MR{Number: 3},  // merged
		&gitlab.MR{Number: 12}, // closed
	})
	require.NoError(t, err)
	assert.Equal(t, []forge.ChangeState{
		forge.ChangeMerged,
		forge.ChangeOpen,
		forge.ChangeMerged,
		forge.ChangeClosed,
	}, states)
}

func TestIntegration_Repository_ListChangeTemplates(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		ctx := t.Context()
		rec := newRecorder(t, t.Name())
		ghc := newGitLabClient(rec.GetDefaultClient())
		repo, err := gitlab.NewRepository(
			ctx, new(gitlab.Forge), "abg", "test-repo",
			silogtest.New(t), ghc,
			&gitlab.RepositoryOptions{RepositoryID: _testRepoID},
		)
		require.NoError(t, err)

		templates, err := repo.ListChangeTemplates(ctx)
		require.NoError(t, err)
		assert.Empty(t, templates)
	})

	t.Run("present", func(t *testing.T) {
		ctx := t.Context()
		rec := newRecorder(t, t.Name())
		ghc := newGitLabClient(rec.GetDefaultClient())
		repo, err := gitlab.NewRepository(ctx, new(gitlab.Forge), "gitlab-org", "cli", silogtest.New(t), ghc, nil)
		require.NoError(t, err)

		templates, err := repo.ListChangeTemplates(ctx)
		require.NoError(t, err)
		require.Len(t, templates, 2)

		template := templates[0]
		assert.Equal(t, "Default", template.Filename)
		assert.NotEmpty(t, template.Body)
	})
}

// https://github.com/abhinav/git-spice/issues/931
func TestIntegration_Repository_ListChangeTemplates_empty(t *testing.T) {
	ctx := t.Context()

	emptyTemplateFixture := fixturetest.New(_fixtures, "empty-template", func() string {
		return randomString(8) + ".md"
	})
	nonEmptyTemplateFixture := fixturetest.New(_fixtures, "non-empty-template", func() string {
		return randomString(8) + ".md"
	})

	emptyTemplateName := emptyTemplateFixture.Get(t)
	nonEmptyTemplateName := nonEmptyTemplateFixture.Get(t)

	var commitHash git.Hash
	if gitlab.UpdateFixtures() {
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

		gitWork, err := git.OpenWorktree(ctx, repoDir, git.OpenOptions{
			Log: silogtest.New(t),
		})
		require.NoError(t, err, "failed to open git repo")
		gitRepo := gitWork.Repository()

		templateDir := filepath.Join(repoDir, ".gitlab", "merge_request_templates")
		require.NoError(t, os.MkdirAll(templateDir, 0o755), "could not create templates directory")

		// Create empty template.
		require.NoError(t, os.WriteFile(
			filepath.Join(templateDir, emptyTemplateName),
			nil,
			0o644,
		), "could not write empty template")

		// Create non-empty template.
		require.NoError(t, os.WriteFile(
			filepath.Join(templateDir, nonEmptyTemplateName),
			[]byte("This is a test MR template\n"),
			0o644,
		), "could not write non-empty template")

		cmd = exec.Command("git", "add", filepath.Join(".gitlab", "merge_request_templates"))
		cmd.Dir = repoDir
		cmd.Stdout = output
		cmd.Stderr = output
		require.NoError(t, cmd.Run(), "git add failed")

		require.NoError(t, gitWork.Commit(ctx, git.CommitRequest{
			Message: "Add empty and non-empty MR templates",
		}), "could not commit templates")

		commitHash, err = gitRepo.PeelToCommit(ctx, "HEAD")
		require.NoError(t, err, "could not get commit hash")

		t.Logf("Pushing templates to main")
		require.NoError(t,
			gitWork.Push(ctx, git.PushOptions{
				Remote:  "origin",
				Refspec: git.Refspec("main"),
			}), "error pushing templates")

		t.Cleanup(func() {
			ctx := context.WithoutCancel(t.Context())
			t.Logf("Reverting template commit")

			cmd := exec.Command("git", "revert", "--no-edit", commitHash.String())
			cmd.Dir = repoDir
			cmd.Stdout = output
			cmd.Stderr = output
			assert.NoError(t, cmd.Run(), "could not revert commit")

			assert.NoError(t,
				gitWork.Push(ctx, git.PushOptions{
					Remote:  "origin",
					Refspec: git.Refspec("main"),
				}), "error pushing revert")
		})
	}

	rec := newRecorder(t, t.Name())
	ghc := newGitLabClient(rec.GetDefaultClient())
	repo, err := gitlab.NewRepository(
		ctx, new(gitlab.Forge), "abg", "test-repo",
		silogtest.New(t), ghc,
		&gitlab.RepositoryOptions{RepositoryID: _testRepoID},
	)
	require.NoError(t, err)

	templates, err := repo.ListChangeTemplates(ctx)
	require.NoError(t, err)

	// Find our test templates in the results.
	// GitLab doesn't use extensions in template names, so strip ".md".
	var foundEmpty, foundNonEmpty bool
	wantEmptyTemplate := strings.TrimSuffix(emptyTemplateName, ".md")
	wantNonEmptyTemplate := strings.TrimSuffix(nonEmptyTemplateName, ".md")
	for _, template := range templates {
		t.Logf("Found template: %s", template.Filename)
		if template.Filename == wantEmptyTemplate {
			foundEmpty = true
			assert.Empty(t, template.Body, "empty template should have empty body")
		}
		if template.Filename == wantNonEmptyTemplate {
			foundNonEmpty = true
			assert.NotEmpty(t, template.Body, "non-empty template should have non-empty body")
		}
	}

	assert.True(t, foundEmpty, "empty template not found in results")
	assert.True(t, foundNonEmpty, "non-empty template not found in results")
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

func TestIntegration_Repository_SubmitEditChange(t *testing.T) {
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
	if gitlab.UpdateFixtures() {
		t.Setenv("GIT_AUTHOR_EMAIL", "bot@example.com")
		t.Setenv("GIT_AUTHOR_NAME", "gs-test[bot]")
		t.Setenv("GIT_COMMITTER_EMAIL", "bot@example.com")
		t.Setenv("GIT_COMMITTER_NAME", "gs-test[bot]")

		output := t.Output()

		t.Logf("Cloning test-repo...")
		repoDir := t.TempDir()
		cmd := exec.Command("git", "clone", "git@gitlab.com:abg/test-repo.git", repoDir)
		cmd.Stdout = output
		cmd.Stdout = output
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
		ctx, new(gitlab.Forge), "abg", "test-repo", silogtest.New(t), ghc, &gitlab.RepositoryOptions{RepositoryID: _testRepoID},
	)
	require.NoError(t, err)

	change, err := repo.SubmitChange(ctx, forge.SubmitChangeRequest{
		Subject: branchName,
		Body:    "Test PR",
		Base:    "main",
		Head:    branchName,
	})
	require.NoError(t, err, "error creating PR")
	changeID := change.ID

	t.Run("ChangeBase", func(t *testing.T) {
		ctx := t.Context()
		newBaseFixture := fixturetest.New(_fixtures, "new-base", func() string {
			return randomString(8)
		})

		newBase := newBaseFixture.Get(t)
		t.Logf("Pushing new base: %s", newBase)
		if gitlab.UpdateFixtures() {
			require.NoError(t,
				gitWork.Push(ctx, git.PushOptions{
					Remote:  "origin",
					Refspec: git.Refspec("main:" + newBase),
				}), "could not push base branch")

			t.Cleanup(func() {
				ctx := context.WithoutCancel(t.Context())
				t.Logf("Deleting remote branch: %s", newBase)
				require.NoError(t,
					gitWork.Push(ctx, git.PushOptions{
						Remote:  "origin",
						Refspec: git.Refspec(":" + newBase),
					}), "error deleting branch")
			})
		}

		t.Logf("Changing base to: %s", newBase)
		require.NoError(t,
			repo.EditChange(ctx, changeID, forge.EditChangeOptions{
				Base: newBase,
			}), "could not update base branch for PR")
		t.Cleanup(func() {
			ctx := context.WithoutCancel(t.Context())
			t.Logf("Changing base back to: main")
			require.NoError(t,
				repo.EditChange(ctx, changeID, forge.EditChangeOptions{
					Base: "main",
				}), "error restoring base branch")
		})

		change, err := repo.FindChangeByID(ctx, changeID)
		require.NoError(t, err, "could not find PR after changing base")

		assert.Equal(t, newBase, change.BaseName,
			"base change did not take effect")
	})

	t.Run("ChangeDraft", func(t *testing.T) {
		ctx := t.Context()
		t.Logf("Changing to draft")
		draft := true
		require.NoError(t,
			repo.EditChange(ctx, changeID, forge.EditChangeOptions{
				Draft: &draft,
			}), "could not update draft status for PR")
		t.Cleanup(func() {
			ctx := context.WithoutCancel(t.Context())
			t.Logf("Changing to ready for review")
			draft = false
			require.NoError(t,
				repo.EditChange(ctx, changeID, forge.EditChangeOptions{
					Draft: &draft,
				}), "error restoring draft status")
		})

		change, err := repo.FindChangeByID(ctx, changeID)
		require.NoError(t, err, "could not find PR after changing draft")
		assert.True(t, change.Draft, "draft change did not take effect")
	})
}

func TestIntegration_Repository_SubmitEditChange_labels(t *testing.T) {
	label1 := fixturetest.New(_fixtures, "label1", func() string { return randomString(8) }).Get(t)
	label2 := fixturetest.New(_fixtures, "label2", func() string { return randomString(8) }).Get(t)
	label3 := fixturetest.New(_fixtures, "label3", func() string { return randomString(8) }).Get(t)

	branchFixture := fixturetest.New(_fixtures, "branch", func() string {
		return randomString(8)
	})

	branchName := branchFixture.Get(t)
	t.Logf("Creating branch: %s", branchName)

	var (
		gitRepo *git.Repository // only when _update is true
		gitWork *git.Worktree
	)
	if gitlab.UpdateFixtures() {
		t.Setenv("GIT_AUTHOR_EMAIL", "bot@example.com")
		t.Setenv("GIT_AUTHOR_NAME", "gs-test[bot]")
		t.Setenv("GIT_COMMITTER_EMAIL", "bot@example.com")
		t.Setenv("GIT_COMMITTER_NAME", "gs-test[bot]")

		output := t.Output()

		ctx := t.Context()

		t.Logf("Cloning test-repo...")
		repoDir := t.TempDir()
		cmd := exec.Command("git", "clone", "git@gitlab.com:abg/test-repo.git", repoDir)
		cmd.Stdout = output
		cmd.Stdout = output
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

	ctx := t.Context()
	rec := newRecorder(t, t.Name())
	ghc := newGitLabClient(rec.GetDefaultClient())
	repo, err := gitlab.NewRepository(
		ctx, new(gitlab.Forge), "abg", "test-repo", silogtest.New(t), ghc, &gitlab.RepositoryOptions{RepositoryID: _testRepoID},
	)
	require.NoError(t, err)

	change, err := repo.SubmitChange(ctx, forge.SubmitChangeRequest{
		Subject: branchName,
		Body:    "Test PR",
		Base:    "main",
		Head:    branchName,
		Labels:  []string{label1},
	})
	require.NoError(t, err, "error creating PR")
	changeID := change.ID

	t.Run("AddNewLabel", func(t *testing.T) {
		require.NoError(t,
			repo.EditChange(t.Context(), changeID, forge.EditChangeOptions{
				Labels: []string{label2},
			}), "could not add labels to PR")
	})

	t.Run("AddExistingLabel", func(t *testing.T) {
		require.NoError(t,
			repo.EditChange(t.Context(), changeID, forge.EditChangeOptions{
				Labels: []string{label2, label3},
			}), "could not add existing label to PR")
	})

	gotLabels, err := repo.ChangeLabels(t.Context(), changeID)
	require.NoError(t, err, "could not get labels for PR")
	assert.ElementsMatch(t, []string{label1, label2, label3}, gotLabels)
}

func TestIntegration_Repository_FindChangeWithLabels(t *testing.T) {
	label1 := fixturetest.New(_fixtures, "findChangeLabel1", func() string { return randomString(8) }).Get(t)
	label2 := fixturetest.New(_fixtures, "findChangeLabel2", func() string { return randomString(8) }).Get(t)

	branchFixture := fixturetest.New(_fixtures, "findChangeBranch", func() string {
		return randomString(8)
	})

	branchName := branchFixture.Get(t)
	t.Logf("Creating branch: %s", branchName)

	var (
		gitRepo *git.Repository // only when _update is true
		gitWork *git.Worktree
	)
	if gitlab.UpdateFixtures() {
		t.Setenv("GIT_AUTHOR_EMAIL", "bot@example.com")
		t.Setenv("GIT_AUTHOR_NAME", "gs-test[bot]")
		t.Setenv("GIT_COMMITTER_EMAIL", "bot@example.com")
		t.Setenv("GIT_COMMITTER_NAME", "gs-test[bot]")

		output := t.Output()

		ctx := t.Context()

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

	ctx := t.Context()
	rec := newRecorder(t, t.Name())
	glc := newGitLabClient(rec.GetDefaultClient())
	repo, err := gitlab.NewRepository(
		ctx, new(gitlab.Forge), "abg", "test-repo", silogtest.New(t), glc, &gitlab.RepositoryOptions{RepositoryID: _testRepoID},
	)
	require.NoError(t, err)

	// Create a change with labels
	change, err := repo.SubmitChange(ctx, forge.SubmitChangeRequest{
		Subject: branchName,
		Body:    "Test MR with labels",
		Base:    "main",
		Head:    branchName,
		Labels:  []string{label1, label2},
	})
	require.NoError(t, err, "error creating MR")
	changeID := change.ID

	// Find the change by ID and verify labels are present
	foundChange, err := repo.FindChangeByID(ctx, changeID)
	require.NoError(t, err, "error finding change by ID")
	assert.ElementsMatch(t, []string{label1, label2}, foundChange.Labels)

	// Find the change by branch and verify labels are present
	foundChanges, err := repo.FindChangesByBranch(ctx, branchName, forge.FindChangesOptions{})
	require.NoError(t, err, "error finding changes by branch")
	require.Len(t, foundChanges, 1, "expected exactly one change")
	assert.ElementsMatch(t, []string{label1, label2}, foundChanges[0].Labels)
}

func TestIntegration_Repository_comments(t *testing.T) {
	ctx := t.Context()
	rec := newRecorder(t, t.Name())
	ghc := newGitLabClient(rec.GetDefaultClient())
	repo, err := gitlab.NewRepository(
		ctx, new(gitlab.Forge), "abg", "test-repo", silogtest.New(t), ghc, &gitlab.RepositoryOptions{RepositoryID: _testRepoID},
	)
	require.NoError(t, err)

	commentBody := fixturetest.New(_fixtures, "comment", func() string {
		return randomString(32)
	}).Get(t)
	commentID, err := repo.PostChangeComment(ctx, &gitlab.MR{
		Number: 4,
	}, commentBody)
	require.NoError(t, err, "could not post comment")
	t.Cleanup(func() {
		t.Logf("Deleting comment: %s", commentID)

		ctx := context.WithoutCancel(t.Context())
		require.NoError(t,
			repo.DeleteChangeComment(ctx, commentID),
			"could not delete comment")
	})

	t.Run("UpdateChangeComment", func(t *testing.T) {
		ctx := t.Context()
		newCommentBody := fixturetest.New(_fixtures, "new-comment", func() string {
			return randomString(32)
		}).Get(t)

		require.NoError(t,
			repo.UpdateChangeComment(ctx, commentID, newCommentBody),
			"could not update comment")
	})
}

func TestIntegration_Repository_ListChangeComments_simple(t *testing.T) {
	prID := &gitlab.MR{Number: 4}

	ctx := t.Context()
	rec := newRecorder(t, t.Name())
	ghc := newGitLabClient(rec.GetDefaultClient())
	repo, err := gitlab.NewRepository(
		ctx, new(gitlab.Forge), "abg", "test-repo", silogtest.New(t), ghc, &gitlab.RepositoryOptions{RepositoryID: _testRepoID},
	)
	require.NoError(t, err)

	listOpts := &forge.ListChangeCommentsOptions{
		CanUpdate: true,
		BodyMatchesAll: []*regexp.Regexp{
			regexp.MustCompile(`(?m)^This change is part of the following stack:$`),
			regexp.MustCompile(`- !4`),
		},
	}
	var items []*forge.ListChangeCommentItem
	for comment, err := range repo.ListChangeComments(ctx, prID, listOpts) {
		require.NoError(t, err)
		items = append(items, comment)
	}

	assert.Equal(t, []*forge.ListChangeCommentItem{
		{
			ID: &gitlab.MRComment{
				Number:   2225710594,
				MRNumber: 4,
			},
			Body: "This change is part of the following stack:\n\n" +
				"- !4 ◀\n\n" +
				"<sub>Change managed by [git-spice](https://abhinav.github.io/git-spice/).</sub>\n" +
				"<!-- gs:navigation comment -->",
		},
	}, items)
}

func TestIntegration_Repository_ListChangeComments_paginated(t *testing.T) {
	const TotalComments = 10
	gitlab.SetListChangeCommentsPageSize(t, 3)

	// https://gitlab.com/abg/test-repo/-/merge_requests/7
	prID := &gitlab.MR{
		Number: 7,
	}

	ctx := t.Context()
	rec := newRecorder(t, t.Name())
	ghc := newGitLabClient(rec.GetDefaultClient())
	repo, err := gitlab.NewRepository(
		ctx, new(gitlab.Forge), "abg", "test-repo", silogtest.New(t), ghc, &gitlab.RepositoryOptions{RepositoryID: _testRepoID},
	)
	require.NoError(t, err)

	comments := fixturetest.New(_fixtures, "comments", func() []string {
		comments := make([]string, TotalComments)
		for i := range comments {
			comments[i] = randomString(32)
		}
		return comments
	}).Get(t)

	var commentIDs []forge.ChangeCommentID
	t.Cleanup(func() {
		ctx := context.WithoutCancel(t.Context())
		for _, commentID := range commentIDs {
			t.Logf("Deleting comment: %s", commentID)

			assert.NoError(t,
				repo.DeleteChangeComment(ctx, commentID),
				"could not delete comment")
		}
	})

	// Post the comments before listing them.
	for _, comment := range comments {
		commentID, err := repo.PostChangeComment(ctx, prID, comment)
		require.NoError(t, err, "could not post comment")
		t.Logf("Posted comment: %s", commentID)
		commentIDs = append(commentIDs, commentID)
	}

	var gotBodies []string
	for comment, err := range repo.ListChangeComments(ctx, prID, nil /* opts */) {
		require.NoError(t, err)
		gotBodies = append(gotBodies, comment.Body)
	}

	assert.Len(t, gotBodies, TotalComments)
	assert.ElementsMatch(t, comments, gotBodies)
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
	if gitlab.UpdateFixtures() {
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
