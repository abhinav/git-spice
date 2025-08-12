package shamhub

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestForkWorkflow(t *testing.T) {
	t.Setenv("EDITOR", "false") // no editor popups
	t.Setenv("USER", "test")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")

	ctx := context.Background()

	// Setup ShamHub server
	sh, err := New(Config{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, sh.Close())
	}()

	// Register users and get tokens
	require.NoError(t, sh.RegisterUser("alice"))
	require.NoError(t, sh.RegisterUser("bob"))

	aliceToken := loginAndGetToken(t, sh, "alice")

	// Set up Alice's repository with an initial commit.
	aliceRepoURL, err := sh.NewRepository("alice", "store")
	require.NoError(t, err)

	aliceWorkDir := t.TempDir()
	aliceWorktree, err := git.Clone(ctx, aliceRepoURL, aliceWorkDir, git.CloneOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(
		filepath.Join(aliceWorkDir, "feature1.txt"),
		[]byte("Alice's feature 1"), 0o644,
	))
	gitAdd(t, aliceWorkDir, "feature1.txt")
	require.NoError(t, aliceWorktree.Commit(ctx, git.CommitRequest{
		Message: "Add feature1.txt",
	}))
	require.NoError(t, aliceWorktree.Push(ctx, git.PushOptions{
		Remote:  "origin",
		Refspec: "main:main",
	}))

	// Fork alice/store to bob/store,
	// and make a change on a feature branch.
	bobRepoURL, err := sh.ForkRepository("alice", "store", "bob")
	require.NoError(t, err)

	require.NotEqual(t, aliceRepoURL, bobRepoURL)

	bobWorkDir := t.TempDir()
	bobWorktree, err := git.Clone(ctx, bobRepoURL, bobWorkDir, git.CloneOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	bobRepo := bobWorktree.Repository()
	err = bobRepo.CreateBranch(ctx, git.CreateBranchRequest{
		Name: "feature2",
		Head: "HEAD",
	})
	require.NoError(t, err)

	// Set up bob's feature2 branch.
	{
		require.NoError(t, bobWorktree.Checkout(ctx, "feature2"))
		require.NoError(t, os.WriteFile(
			filepath.Join(bobWorkDir, "feature2.txt"),
			[]byte("Bob's feature 2"), 0o644),
		)
		gitAdd(t, bobWorkDir, "feature2.txt")
		require.NoError(t, bobWorktree.Commit(ctx, git.CommitRequest{
			Message: "Add feature2.txt",
		}))
		require.NoError(t, bobWorktree.Push(ctx, git.PushOptions{
			Remote:  "origin",
			Refspec: "feature2:feature2",
		}))
	}

	// Submit change request to merge bob's feature2 into alice's main
	changeNumber := func() int {
		reqBody, err := json.Marshal(submitChangeRequest{
			Subject:  "Add feature2 from bob's fork",
			Body:     "This PR adds feature2.txt from Bob's fork to Alice's repository",
			Base:     "main",
			Head:     "feature2",
			HeadRepo: "bob/store",
		})
		require.NoError(t, err)

		req, err := http.NewRequest("POST", sh.APIURL()+"/alice/store/changes", bytes.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authentication-Token", aliceToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		require.Equal(t, http.StatusOK, resp.StatusCode)

		var changeResp submitChangeResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&changeResp))
		return changeResp.Number
	}()

	// Merge the change request with DeleteBranch=true
	require.NoError(t, sh.MergeChange(MergeChangeRequest{
		Owner:        "alice",
		Repo:         "store",
		Number:       changeNumber,
		DeleteBranch: true,
	}))

	require.NoError(t, aliceWorktree.Pull(ctx, git.PullOptions{}))

	// Check that feature2.txt now exists in alice's repo
	feature2Content, err := os.ReadFile(filepath.Join(aliceWorkDir, "feature2.txt"))
	require.NoError(t, err)
	assert.Equal(t, "Bob's feature 2", string(feature2Content))

	// Verification:
	// Check that the feature2 branch was deleted from bob's repo
	bobRepo = bobWorktree.Repository()
	require.NoError(t, bobRepo.Fetch(ctx, git.FetchOptions{
		Remote: "origin",
	}))

	// List remote refs to verify origin/feature2 no longer exists
	var remoteRefs []string
	for remoteRef, err := range bobRepo.ListRemoteRefs(ctx, "origin", &git.ListRemoteRefsOptions{}) {
		require.NoError(t, err)
		remoteRefs = append(remoteRefs, remoteRef.Name)
	}

	// feature2 should not exist as a remote branch anymore
	assert.NotContains(t, remoteRefs, "refs/heads/feature2")
}

// gitAdd is a helper function to add files to the git index
func gitAdd(t *testing.T, workDir, filename string) {
	cmd := exec.Command("git", "add", filename)
	cmd.Dir = workDir
	require.NoError(t, cmd.Run())
}

// loginAndGetToken logs in as a user and returns the authentication token
func loginAndGetToken(t *testing.T, sh *ShamHub, username string) string {
	loginReq := loginRequest{Username: username}
	reqBody, err := json.Marshal(loginReq)
	require.NoError(t, err)

	resp, err := http.Post(sh.APIURL()+"/login", "application/json", bytes.NewReader(reqBody))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var loginResp loginResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&loginResp))
	return loginResp.Token
}
