package github_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"flag"
	"io"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shurcooL/githubv4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/fixturetest"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/github"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/ioutil"
	"go.abhg.dev/gs/internal/logtest"
	"golang.org/x/oauth2"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

// This file tests basic, end-to-end interactions with the GitHub API
// using recorded fixtures.

var (
	_update   = flag.Bool("update", false, "update test fixtures")
	_fixtures = fixturetest.Config{Update: _update}
)

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

	var realTransport http.RoundTripper
	afterCaptureHook := func(*cassette.Interaction) error { return nil }
	mode := recorder.ModeReplayOnly
	if *_update {
		mode = recorder.ModeRecordOnly
		githubToken := os.Getenv("GITHUB_TOKEN")
		require.NotEmpty(t, githubToken,
			"$GITHUB_TOKEN must be set in record mode")

		realTransport = &oauth2.Transport{
			Source: oauth2.StaticTokenSource(&oauth2.Token{
				AccessToken: githubToken,
			}),
		}

		// Because the oauth transport is the inner transport,
		// the recorder will never see the Authorization header.
		// But for extra paranoia, we'll strip all but a handful
		// of headers from the request and response before saving.
		afterCaptureHook = func(i *cassette.Interaction) error {
			allHeaders := make(http.Header)
			maps.Copy(allHeaders, i.Request.Headers)
			maps.Copy(allHeaders, i.Response.Headers)

			var toRemove []string
			for k := range allHeaders {
				switch strings.ToLower(k) {
				case "content-type", "content-length", "user-agent":
					// ok
				default:
					toRemove = append(toRemove, k)
				}
			}

			for _, k := range toRemove {
				delete(i.Request.Headers, k)
				delete(i.Response.Headers, k)
			}

			return nil
		}
	}

	rec, err := recorder.New(filepath.Join("testdata", "fixtures", name),
		recorder.WithMode(mode),
		recorder.WithRealTransport(realTransport),
		recorder.WithSkipRequestLatency(true),
		recorder.WithHook(afterCaptureHook, recorder.AfterCaptureHook),

		// GraphQL requests will all have the same method and URL.
		// We'll need to match the body instead.
		recorder.WithMatcher(func(r *http.Request, i cassette.Request) bool {
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
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, rec.Stop())
	})

	return rec
}

func TestIntegration_Repository(t *testing.T) {
	ctx := context.Background()
	rec := newRecorder(t, t.Name())
	ghc := githubv4.NewClient(rec.GetDefaultClient())
	_, err := github.NewRepository(ctx, new(github.Forge), "abhinav", "git-spice", logtest.New(t), ghc, nil)
	require.NoError(t, err)
}

func TestIntegration_Repository_FindChangeByID(t *testing.T) {
	ctx := context.Background()
	rec := newRecorder(t, t.Name())
	ghc := githubv4.NewClient(rec.GetDefaultClient())
	repo, err := github.NewRepository(ctx, new(github.Forge), "abhinav", "git-spice", logtest.New(t), ghc, _gitSpiceRepoID)
	require.NoError(t, err)

	t.Run("found", func(t *testing.T) {
		change, err := repo.FindChangeByID(ctx, &github.PR{Number: 141})
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
		_, err := repo.FindChangeByID(ctx, &github.PR{Number: 999})
		require.Error(t, err)
		assert.ErrorContains(t, err, "Could not resolve")
	})
}

func TestIntegration_Repository_FindChangesByBranch(t *testing.T) {
	ctx := context.Background()
	rec := newRecorder(t, t.Name())
	ghc := githubv4.NewClient(rec.GetDefaultClient())
	repo, err := github.NewRepository(ctx, new(github.Forge), "abhinav", "git-spice", logtest.New(t), ghc, _gitSpiceRepoID)
	require.NoError(t, err)

	t.Run("found", func(t *testing.T) {
		changes, err := repo.FindChangesByBranch(ctx, "gh-graphql", forge.FindChangesOptions{})
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
		changes, err := repo.FindChangesByBranch(ctx, "does-not-exist", forge.FindChangesOptions{})
		require.NoError(t, err)
		assert.Empty(t, changes)
	})
}

func TestIntegration_Repository_IsMerged(t *testing.T) {
	ctx := context.Background()
	rec := newRecorder(t, t.Name())
	ghc := githubv4.NewClient(rec.GetDefaultClient())
	repo, err := github.NewRepository(ctx, new(github.Forge), "abhinav", "git-spice", logtest.New(t), ghc, _gitSpiceRepoID)
	require.NoError(t, err)

	t.Run("false", func(t *testing.T) {
		ok, err := repo.ChangeIsMerged(ctx, &github.PR{Number: 144})
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("true", func(t *testing.T) {
		ok, err := repo.ChangeIsMerged(ctx, &github.PR{Number: 141})
		require.NoError(t, err)
		assert.True(t, ok)
	})
}

func TestIntegration_Repository_ListChangeTemplates(t *testing.T) {
	ctx := context.Background()

	t.Run("absent", func(t *testing.T) {
		rec := newRecorder(t, t.Name())
		ghc := githubv4.NewClient(rec.GetDefaultClient())
		repo, err := github.NewRepository(ctx, new(github.Forge), "abhinav", "git-spice", logtest.New(t), ghc, _gitSpiceRepoID)
		require.NoError(t, err)

		templates, err := repo.ListChangeTemplates(ctx)
		require.NoError(t, err)
		assert.Empty(t, templates)
	})

	t.Run("present", func(t *testing.T) {
		rec := newRecorder(t, t.Name())
		ghc := githubv4.NewClient(rec.GetDefaultClient())
		repo, err := github.NewRepository(ctx, new(github.Forge), "golang", "go", logtest.New(t), ghc, nil)
		require.NoError(t, err)

		templates, err := repo.ListChangeTemplates(ctx)
		require.NoError(t, err)
		require.Len(t, templates, 1)

		template := templates[0]
		assert.Equal(t, "PULL_REQUEST_TEMPLATE", template.Filename)
		assert.NotEmpty(t, template.Body)
	})
}

func TestIntegration_Repository_NewChangeMetadata(t *testing.T) {
	ctx := context.Background()

	rec := newRecorder(t, t.Name())
	ghc := githubv4.NewClient(rec.GetDefaultClient())
	repo, err := github.NewRepository(ctx, new(github.Forge), "abhinav", "git-spice", logtest.New(t), ghc, _gitSpiceRepoID)
	require.NoError(t, err)

	t.Run("valid", func(t *testing.T) {
		md, err := repo.NewChangeMetadata(ctx, &github.PR{Number: 196})
		require.NoError(t, err)

		assert.Equal(t, &github.PR{
			Number: 196,
			GQLID:  "PR_kwDOJ2BQKs5ylEYu",
		}, md.ChangeID())
		assert.Equal(t, "github", md.ForgeID())
	})

	t.Run("invalid", func(t *testing.T) {
		_, err := repo.NewChangeMetadata(ctx, &github.PR{Number: 10000})
		require.Error(t, err)
		assert.ErrorContains(t, err, "get pull request ID")
	})
}

func TestIntegration_Repository_SubmitEditChange(t *testing.T) {
	ctx := context.Background()
	branchFixture := fixturetest.New(_fixtures, "branch", func() string {
		return randomString(8)
	})

	branchName := branchFixture.Get(t)
	t.Logf("Creating branch: %s", branchName)

	var gitRepo *git.Repository // only when _update is true
	if *_update {
		t.Setenv("GIT_AUTHOR_EMAIL", "bot@example.com")
		t.Setenv("GIT_AUTHOR_NAME", "gs-test[bot]")
		t.Setenv("GIT_COMMITTER_EMAIL", "bot@example.com")
		t.Setenv("GIT_COMMITTER_NAME", "gs-test[bot]")

		output := ioutil.TestOutputWriter(t, "[git] ")

		t.Logf("Cloning test-repo...")
		repoDir := t.TempDir()
		cmd := exec.Command("git", "clone", "https://github.com/abhinav/test-repo", repoDir)
		cmd.Stdout = output
		cmd.Stdout = output
		require.NoError(t, cmd.Run(), "failed to clone test-repo")

		var err error
		gitRepo, err = git.Open(ctx, repoDir, git.OpenOptions{
			Log: logtest.New(t),
		})
		require.NoError(t, err, "failed to open git repo")

		require.NoError(t, gitRepo.CreateBranch(ctx, git.CreateBranchRequest{
			Name: branchName,
		}), "could not create branch: %s", branchName)
		require.NoError(t, gitRepo.Checkout(ctx, branchName),
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
		require.NoError(t, gitRepo.Commit(ctx, git.CommitRequest{
			Message: "commit from test",
		}), "could not commit changes")

		t.Logf("Pushing to origin")
		require.NoError(t,
			gitRepo.Push(ctx, git.PushOptions{
				Remote:  "origin",
				Refspec: git.Refspec(branchName),
			}), "error pushing branch")

		t.Cleanup(func() {
			t.Logf("Deleting remote branch: %s", branchName)
			assert.NoError(t,
				gitRepo.Push(ctx, git.PushOptions{
					Remote:  "origin",
					Refspec: git.Refspec(":" + branchName),
				}), "error deleting branch")
		})
	}

	rec := newRecorder(t, t.Name())
	ghc := githubv4.NewClient(rec.GetDefaultClient())
	repo, err := github.NewRepository(
		ctx, new(github.Forge), "abhinav", "test-repo", logtest.New(t), ghc, _testRepoID,
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
		newBaseFixture := fixturetest.New(_fixtures, "new-base", func() string {
			return randomString(8)
		})

		newBase := newBaseFixture.Get(t)
		t.Logf("Pushing new base: %s", newBase)
		if *_update {
			require.NoError(t,
				gitRepo.Push(ctx, git.PushOptions{
					Remote:  "origin",
					Refspec: git.Refspec("main:" + newBase),
				}), "could not push base branch")

			t.Cleanup(func() {
				t.Logf("Deleting remote branch: %s", newBase)
				require.NoError(t,
					gitRepo.Push(ctx, git.PushOptions{
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
		t.Logf("Changing to draft")
		draft := true
		require.NoError(t,
			repo.EditChange(ctx, changeID, forge.EditChangeOptions{
				Draft: &draft,
			}), "could not update draft status for PR")
		t.Cleanup(func() {
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

func TestIntegration_Repository_comments(t *testing.T) {
	ctx := context.Background()
	rec := newRecorder(t, t.Name())
	ghc := githubv4.NewClient(rec.GetDefaultClient())
	repo, err := github.NewRepository(
		ctx, new(github.Forge), "abhinav", "test-repo", logtest.New(t), ghc, _testRepoID,
	)
	require.NoError(t, err)

	commentBody := fixturetest.New(_fixtures, "comment", func() string {
		return randomString(32)
	}).Get(t)
	commentID, err := repo.PostChangeComment(ctx, &github.PR{
		Number: 4,
		GQLID:  githubv4.ID("PR_kwDOMVd0xs51N_9r"),
	}, commentBody)
	require.NoError(t, err, "could not post comment")
	t.Cleanup(func() {
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
			repo.UpdateChangeComment(ctx, commentID, newCommentBody),
			"could not update comment")
	})
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
