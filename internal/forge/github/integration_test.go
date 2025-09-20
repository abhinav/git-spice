package github_test

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
	"testing"

	"github.com/shurcooL/githubv4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/fixturetest"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/github"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/graphqlutil"
	"go.abhg.dev/gs/internal/httptest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"golang.org/x/oauth2"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

// This file tests basic, end-to-end interactions with the GitHub API
// using recorded fixtures.

var _fixtures = fixturetest.Config{Update: github.UpdateFixtures}

// To avoid looking this up for every test that needs the repo ID,
// we'll just hardcode it here.
var (
	_gitSpiceRepoID = githubv4.ID("R_kgDOJ2BQKg")
	_testRepoID     = githubv4.ID("R_kgDOMVd0xg")
)

func newRecorder(t *testing.T, name string) *recorder.Recorder {
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("To update the test fixtures, run:")
			t.Logf("    GITHUB_TOKEN=$token go test -update -run '^%s$'", t.Name())
		}
	})

	return httptest.NewTransportRecorder(t, name, httptest.TransportRecorderOptions{
		Update: github.UpdateFixtures,
		WrapRealTransport: func(t testing.TB, transport http.RoundTripper) http.RoundTripper {
			githubToken := os.Getenv("GITHUB_TOKEN")
			require.NotEmpty(t, githubToken,
				"$GITHUB_TOKEN must be set in record mode")

			return &oauth2.Transport{
				Base: transport,
				Source: oauth2.StaticTokenSource(&oauth2.Token{
					AccessToken: githubToken,
				}),
			}
		},
		// GraphQL requests will all have the same method and URL.
		// We'll need to match the body instead.
		Matcher: func(r *http.Request, i cassette.Request) bool {
			if r.Body == nil || r.Body == http.NoBody {
				return cassette.DefaultMatcher(r, i)
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

func newGitHubClient(
	httpClient *http.Client,
) *githubv4.Client {
	httpClient.Transport = graphqlutil.WrapTransport(httpClient.Transport)
	return githubv4.NewClient(httpClient)
}

func TestIntegration_Repository(t *testing.T) {
	rec := newRecorder(t, t.Name())
	ghc := newGitHubClient(rec.GetDefaultClient())
	_, err := github.NewRepository(t.Context(), new(github.Forge), "abhinav", "git-spice", silogtest.New(t), ghc, nil)
	require.NoError(t, err)
}

func TestIntegration_Repository_FindChangeByID(t *testing.T) {
	rec := newRecorder(t, t.Name())
	ghc := newGitHubClient(rec.GetDefaultClient())
	repo, err := github.NewRepository(t.Context(), new(github.Forge), "abhinav", "git-spice", silogtest.New(t), ghc, _gitSpiceRepoID)
	require.NoError(t, err)

	t.Run("found", func(t *testing.T) {
		change, err := repo.FindChangeByID(t.Context(), &github.PR{Number: 141})
		require.NoError(t, err)

		assert.Equal(t, &forge.FindChangeItem{
			ID: &github.PR{
				Number: 141,
				GQLID:  "PR_kwDOJ2BQKs5xNT-u",
			},
			URL:      "https://github.com/abhinav/git-spice/pull/141",
			Subject:  "branch submit: Heal from external PR submissions",
			State:    forge.ChangeMerged,
			BaseName: "main",
			HeadHash: "df0289d83ffae816105947875db01c992224913d",
			Draft:    false,
		}, change)
	})

	t.Run("not-found", func(t *testing.T) {
		_, err := repo.FindChangeByID(t.Context(), &github.PR{Number: 999})
		require.Error(t, err)
		assert.ErrorContains(t, err, "Could not resolve")
	})
}

func TestIntegration_Repository_FindChangesByBranch(t *testing.T) {
	rec := newRecorder(t, t.Name())
	ghc := newGitHubClient(rec.GetDefaultClient())
	repo, err := github.NewRepository(t.Context(), new(github.Forge), "abhinav", "git-spice", silogtest.New(t), ghc, _gitSpiceRepoID)
	require.NoError(t, err)

	t.Run("found", func(t *testing.T) {
		changes, err := repo.FindChangesByBranch(t.Context(), "gh-graphql", forge.FindChangesOptions{})
		require.NoError(t, err)
		assert.Equal(t, []*forge.FindChangeItem{
			{
				ID: &github.PR{
					Number: 144,
					GQLID:  "PR_kwDOJ2BQKs5xNeqO",
				},
				URL:      "https://github.com/abhinav/git-spice/pull/144",
				State:    forge.ChangeMerged,
				Subject:  "GitHub: Use GraphQL API",
				BaseName: "main",
				HeadHash: "5d74cecfe3cb066044d129232229e07f5d04e194",
				Draft:    false,
			},
		}, changes)
	})

	t.Run("not-found", func(t *testing.T) {
		changes, err := repo.FindChangesByBranch(t.Context(), "does-not-exist", forge.FindChangesOptions{})
		require.NoError(t, err)
		assert.Empty(t, changes)
	})
}

func TestIntegration_Repository_ChangesStates(t *testing.T) {
	rec := newRecorder(t, t.Name())
	ghc := newGitHubClient(rec.GetDefaultClient())
	repo, err := github.NewRepository(t.Context(), new(github.Forge), "abhinav", "git-spice", silogtest.New(t), ghc, _gitSpiceRepoID)
	require.NoError(t, err)

	states, err := repo.ChangesStates(t.Context(), []forge.ChangeID{
		&github.PR{Number: 196, GQLID: "PR_kwDOJ2BQKs5ylEYu"}, // merged
		&github.PR{Number: 536, GQLID: "PR_kwDOJ2BQKs6GYA47"}, // open (not merged)
		&github.PR{Number: 144, GQLID: "PR_kwDOJ2BQKs5xNeqO"}, // merged
		// Explicit GQL ID means we don't need to be in the same repo.
		&github.PR{Number: 4, GQLID: githubv4.ID("PR_kwDOMVd0xs51N_9r")}, // closed (not merged)
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
		rec := newRecorder(t, t.Name())
		ghc := newGitHubClient(rec.GetDefaultClient())
		repo, err := github.NewRepository(t.Context(), new(github.Forge), "abhinav", "git-spice", silogtest.New(t), ghc, _gitSpiceRepoID)
		require.NoError(t, err)

		templates, err := repo.ListChangeTemplates(t.Context())
		require.NoError(t, err)
		assert.Empty(t, templates)
	})

	t.Run("present", func(t *testing.T) {
		rec := newRecorder(t, t.Name())
		ghc := newGitHubClient(rec.GetDefaultClient())
		repo, err := github.NewRepository(t.Context(), new(github.Forge), "golang", "go", silogtest.New(t), ghc, nil)
		require.NoError(t, err)

		templates, err := repo.ListChangeTemplates(t.Context())
		require.NoError(t, err)
		require.Len(t, templates, 1)

		template := templates[0]
		assert.Equal(t, "PULL_REQUEST_TEMPLATE", template.Filename)
		assert.NotEmpty(t, template.Body)
	})
}

func TestIntegration_Repository_NewChangeMetadata(t *testing.T) {
	rec := newRecorder(t, t.Name())
	ghc := newGitHubClient(rec.GetDefaultClient())
	repo, err := github.NewRepository(t.Context(), new(github.Forge), "abhinav", "git-spice", silogtest.New(t), ghc, _gitSpiceRepoID)
	require.NoError(t, err)

	t.Run("valid", func(t *testing.T) {
		md, err := repo.NewChangeMetadata(t.Context(), &github.PR{Number: 196})
		require.NoError(t, err)

		assert.Equal(t, &github.PR{
			Number: 196,
			GQLID:  "PR_kwDOJ2BQKs5ylEYu",
		}, md.ChangeID())
		assert.Equal(t, "github", md.ForgeID())
	})

	t.Run("invalid", func(t *testing.T) {
		_, err := repo.NewChangeMetadata(t.Context(), &github.PR{Number: 10000})
		require.Error(t, err)
		assert.ErrorContains(t, err, "get pull request ID")
	})
}

func TestIntegration_Repository_SubmitEditChange(t *testing.T) {
	branchFixture := fixturetest.New(_fixtures, "branch", func() string {
		return randomString(8)
	})

	branchName := branchFixture.Get(t)
	t.Logf("Creating branch: %s", branchName)

	var (
		gitRepo *git.Repository // only when _update is true
		gitWork *git.Worktree
	)
	if github.UpdateFixtures() {
		t.Setenv("GIT_AUTHOR_EMAIL", "bot@example.com")
		t.Setenv("GIT_AUTHOR_NAME", "gs-test[bot]")
		t.Setenv("GIT_COMMITTER_EMAIL", "bot@example.com")
		t.Setenv("GIT_COMMITTER_NAME", "gs-test[bot]")

		output := t.Output()

		t.Logf("Cloning test-repo...")
		repoDir := t.TempDir()
		cmd := exec.Command("git", "clone", "https://github.com/abhinav/test-repo", repoDir)
		cmd.Stdout = output
		cmd.Stdout = output
		require.NoError(t, cmd.Run(), "failed to clone test-repo")

		ctx := t.Context()

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
			t.Logf("Deleting remote branch: %s", branchName)
			assert.NoError(t,
				gitWork.Push(context.WithoutCancel(ctx), git.PushOptions{
					Remote:  "origin",
					Refspec: git.Refspec(":" + branchName),
				}), "error deleting branch")
		})
	}

	rec := newRecorder(t, t.Name())
	ghc := newGitHubClient(rec.GetDefaultClient())
	repo, err := github.NewRepository(
		t.Context(), new(github.Forge), "abhinav", "test-repo", silogtest.New(t), ghc, _testRepoID,
	)
	require.NoError(t, err)

	change, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: branchName,
		Body:    "Test PR",
		Base:    "main",
		Head:    branchName,
	})
	require.NoError(t, err, "error creating PR")
	changeID := change.ID

	t.Run("ChangeBase", func(t *testing.T) {
		newBaseFixture := fixturetest.New(_fixtures, "new-base", func() string {
			return randomString(8)
		})

		newBase := newBaseFixture.Get(t)
		t.Logf("Pushing new base: %s", newBase)
		if github.UpdateFixtures() {
			require.NoError(t,
				gitWork.Push(t.Context(), git.PushOptions{
					Remote:  "origin",
					Refspec: git.Refspec("main:" + newBase),
				}), "could not push base branch")

			t.Cleanup(func() {
				t.Logf("Deleting remote branch: %s", newBase)
				ctx := context.WithoutCancel(t.Context())
				require.NoError(t,
					gitWork.Push(ctx, git.PushOptions{
						Remote:  "origin",
						Refspec: git.Refspec(":" + newBase),
					}), "error deleting branch")
			})
		}

		t.Logf("Changing base to: %s", newBase)
		require.NoError(t,
			repo.EditChange(t.Context(), changeID, forge.EditChangeOptions{
				Base: newBase,
			}), "could not update base branch for PR")
		t.Cleanup(func() {
			t.Logf("Changing base back to: main")
			ctx := context.WithoutCancel(t.Context())
			require.NoError(t,
				repo.EditChange(ctx, changeID, forge.EditChangeOptions{
					Base: "main",
				}), "error restoring base branch")
		})

		change, err := repo.FindChangeByID(t.Context(), changeID)
		require.NoError(t, err, "could not find PR after changing base")

		assert.Equal(t, newBase, change.BaseName,
			"base change did not take effect")
	})

	t.Run("ChangeDraft", func(t *testing.T) {
		t.Logf("Changing to draft")
		draft := true
		require.NoError(t,
			repo.EditChange(t.Context(), changeID, forge.EditChangeOptions{
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

		change, err := repo.FindChangeByID(t.Context(), changeID)
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
	if github.UpdateFixtures() {
		t.Setenv("GIT_AUTHOR_EMAIL", "bot@example.com")
		t.Setenv("GIT_AUTHOR_NAME", "gs-test[bot]")
		t.Setenv("GIT_COMMITTER_EMAIL", "bot@example.com")
		t.Setenv("GIT_COMMITTER_NAME", "gs-test[bot]")

		output := t.Output()

		t.Logf("Cloning test-repo...")
		repoDir := t.TempDir()
		cmd := exec.Command("git", "clone", "https://github.com/abhinav/test-repo", repoDir)
		cmd.Stdout = output
		cmd.Stdout = output
		require.NoError(t, cmd.Run(), "failed to clone test-repo")

		ctx := t.Context()

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
			t.Logf("Deleting remote branch: %s", branchName)
			assert.NoError(t,
				gitWork.Push(context.WithoutCancel(ctx), git.PushOptions{
					Remote:  "origin",
					Refspec: git.Refspec(":" + branchName),
				}), "error deleting branch")
		})
	}

	rec := newRecorder(t, t.Name())
	ghc := newGitHubClient(rec.GetDefaultClient())
	repo, err := github.NewRepository(
		t.Context(), new(github.Forge), "abhinav", "test-repo", silogtest.New(t), ghc, _testRepoID,
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		labels := []string{label1, label2, label3}
		ctx := context.WithoutCancel(t.Context())
		for _, label := range labels {
			assert.NoError(t,
				repo.DeleteLabel(ctx, label), "could not delete label: %s", label)
		}
	})

	change, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
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
}

func TestIntegration_Repository_LabelCreateDelete(t *testing.T) {
	label := fixturetest.New(_fixtures, "label1", func() string { return randomString(8) }).Get(t)

	rec := newRecorder(t, t.Name())
	ghc := newGitHubClient(rec.GetDefaultClient())
	repo, err := github.NewRepository(
		t.Context(), new(github.Forge), "abhinav", "test-repo", silogtest.New(t), ghc, _testRepoID,
	)
	require.NoError(t, err)

	t.Run("DoesNotExist", func(t *testing.T) {
		_, err := repo.LabelID(t.Context(), label)
		require.Error(t, err, "expected error for non-existent label")
		assert.ErrorIs(t, err, github.ErrLabelNotFound)
	})

	id, err := repo.CreateLabel(t.Context(), label)
	require.NoError(t, err, "could not create label")
	t.Cleanup(func() {
		t.Logf("Deleting label: %s", label)
		ctx := context.WithoutCancel(t.Context())
		assert.NoError(t,
			repo.DeleteLabel(ctx, label), "could not delete label")
	})

	t.Run("LabelID", func(t *testing.T) {
		gotID, err := repo.LabelID(t.Context(), label)
		require.NoError(t, err, "could not get label ID")
		assert.Equal(t, id, gotID, "label ID does not match")
	})

	t.Run("createIsIdempotent", func(t *testing.T) {
		newID, err := repo.CreateLabel(t.Context(), label)
		require.NoError(t, err, "could not create label again")

		assert.Equal(t, id, newID, "label ID should be the same on idempotent create")
	})
}

func TestIntegration_Repository_SubmitChange_baseBranchDoesNotExist(t *testing.T) {
	branchFixture := fixturetest.New(_fixtures, "branch", func() string {
		return randomString(8)
	})
	baseBranchFixture := fixturetest.New(_fixtures, "base-branch", func() string {
		return randomString(8)
	})

	branchName := branchFixture.Get(t)
	baseBranchName := baseBranchFixture.Get(t)
	t.Logf("Creating branch %s with base branch %s", branchName, baseBranchName)

	var (
		gitRepo *git.Repository // only when _update is true
		gitWork *git.Worktree
	)
	if github.UpdateFixtures() {
		t.Setenv("GIT_AUTHOR_EMAIL", "bot@example.com")
		t.Setenv("GIT_AUTHOR_NAME", "gs-test[bot]")
		t.Setenv("GIT_COMMITTER_EMAIL", "bot@example.com")
		t.Setenv("GIT_COMMITTER_NAME", "gs-test[bot]")

		output := t.Output()

		t.Logf("Cloning test-repo...")
		repoDir := t.TempDir()
		cmd := exec.Command("git", "clone", "https://github.com/abhinav/test-repo", repoDir)
		cmd.Stdout = output
		cmd.Stdout = output
		require.NoError(t, cmd.Run(), "failed to clone test-repo")

		ctx := t.Context()

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
			t.Logf("Deleting remote branch: %s", branchName)
			assert.NoError(t,
				gitWork.Push(context.WithoutCancel(ctx), git.PushOptions{
					Remote:  "origin",
					Refspec: git.Refspec(":" + branchName),
				}), "error deleting branch")
		})
	}

	rec := newRecorder(t, t.Name())
	ghc := newGitHubClient(rec.GetDefaultClient())
	repo, err := github.NewRepository(
		t.Context(), new(github.Forge), "abhinav", "test-repo", silogtest.New(t), ghc, _testRepoID,
	)
	require.NoError(t, err)

	_, err = repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: branchName,
		Body:    "Test PR",
		Base:    baseBranchName, // This branch does not exist in remote
		Head:    branchName,
	})
	require.Error(t, err, "error creating PR")
	assert.ErrorIs(t, err, forge.ErrUnsubmittedBase)
}

func TestIntegration_Repository_comments(t *testing.T) {
	rec := newRecorder(t, t.Name())
	ghc := newGitHubClient(rec.GetDefaultClient())
	repo, err := github.NewRepository(
		t.Context(), new(github.Forge), "abhinav", "test-repo", silogtest.New(t), ghc, _testRepoID,
	)
	require.NoError(t, err)

	commentBody := fixturetest.New(_fixtures, "comment", func() string {
		return randomString(32)
	}).Get(t)
	commentID, err := repo.PostChangeComment(t.Context(), &github.PR{
		Number: 4,
		GQLID:  githubv4.ID("PR_kwDOMVd0xs51N_9r"),
	}, commentBody)
	require.NoError(t, err, "could not post comment")
	t.Cleanup(func() {
		ctx := context.WithoutCancel(t.Context())
		t.Logf("Deleting comment: %s", commentID)

		require.NoError(t,
			repo.DeleteChangeComment(ctx, commentID),
			"could not delete comment")
	})

	t.Run("UpdateChangeComment", func(t *testing.T) {
		newCommentBody := fixturetest.New(_fixtures, "new-comment", func() string {
			return randomString(32)
		}).Get(t)

		require.NoError(t,
			repo.UpdateChangeComment(t.Context(), commentID, newCommentBody),
			"could not update comment")
	})
}

func TestIntegration_Repository_ListChangeComments_simple(t *testing.T) {
	const _prGQLID = "PR_kwDOJ2BQKs55Hpxz" // https://github.com/abhinav/git-spice/pull/356
	prID := &github.PR{Number: 356, GQLID: githubv4.ID(_prGQLID)}

	ctx := t.Context()
	rec := newRecorder(t, t.Name())
	ghc := newGitHubClient(rec.GetDefaultClient())
	repo, err := github.NewRepository(
		ctx, new(github.Forge), "abhinav", "git-spice", silogtest.New(t), ghc, _gitSpiceRepoID,
	)
	require.NoError(t, err)

	listOpts := &forge.ListChangeCommentsOptions{
		CanUpdate: true,
		BodyMatchesAll: []*regexp.Regexp{
			regexp.MustCompile(`(?m)^This change is part of the following stack:$`),
			regexp.MustCompile(`- #356`),
		},
	}
	var items []*forge.ListChangeCommentItem
	for comment, err := range repo.ListChangeComments(ctx, prID, listOpts) {
		require.NoError(t, err)
		items = append(items, comment)
	}

	assert.Equal(t, []*forge.ListChangeCommentItem{
		{
			ID: &github.PRComment{
				GQLID: githubv4.ID("IC_kwDOJ2BQKs6JXKfO"),
				URL:   "https://github.com/abhinav/git-spice/pull/356#issuecomment-2304550862",
			},
			Body: "This change is part of the following stack:\n\n" +
				"- #356 ◀\n\n" +
				"<sub>Change managed by [git-spice](https://abhinav.github.io/git-spice/).</sub>\n",
		},
	}, items)
}

func TestIntegration_Repository_ListChangeComments_paginated(t *testing.T) {
	const TotalComments = 10
	github.SetListChangeCommentsPageSize(t, 3)

	// https://github.com/abhinav/test-repo/pull/4
	prID := &github.PR{
		Number: 4,
		GQLID:  githubv4.ID("PR_kwDOMVd0xs51N_9r"),
	}

	ctx := t.Context()
	rec := newRecorder(t, t.Name())
	ghc := newGitHubClient(rec.GetDefaultClient())
	repo, err := github.NewRepository(
		ctx, new(github.Forge), "abhinav", "test-repo", silogtest.New(t), ghc, _testRepoID,
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
	client.Transport = graphqlutil.WrapTransport(client.Transport)
	ghc := newGitHubClient(client)
	_, err := github.NewRepository(ctx, new(github.Forge), "abhinav", "does-not-exist-repo", silogtest.New(t), ghc, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, graphqlutil.ErrNotFound)

	var gqlError *graphqlutil.Error
	if assert.ErrorAs(t, err, &gqlError) {
		assert.Equal(t, "NOT_FOUND", gqlError.Type)
		assert.Equal(t, []any{"repository"}, gqlError.Path)
		assert.Contains(t, gqlError.Message, "abhinav/does-not-exist-repo")
	}
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
