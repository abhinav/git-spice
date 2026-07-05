package shamhub

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestAdminAuth(t *testing.T) {
	sh, err := New(Config{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, sh.Close())
	})

	assert.Equal(t,
		http.StatusUnauthorized,
		adminStatus(t, sh, "", "POST", "/_shamhub/admin/users",
			adminRegisterUserBody{Username: "alice"}),
	)
	assert.Equal(t,
		http.StatusUnauthorized,
		adminStatus(t, sh, "wrong", "POST", "/_shamhub/admin/users",
			adminRegisterUserBody{Username: "alice"}),
	)
	assert.Equal(t,
		http.StatusOK,
		adminStatus(t, sh, sh.AdminToken(), "POST", "/_shamhub/admin/users",
			adminRegisterUserBody{Username: "alice"}),
	)
}

func TestAdminRepositoriesAndConfig(t *testing.T) {
	sh, err := New(Config{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, sh.Close())
	})

	var repo adminRepositoryResponse
	adminRequest(
		t,
		sh,
		http.MethodPost,
		"/_shamhub/admin/repos",
		adminNewRepositoryBody{
			Owner: "alice",
			Repo:  "example",
		},
		&repo,
	)
	assert.Equal(t, sh.RepoURL("alice", "example"), repo.URL)

	var fork adminRepositoryResponse
	adminRequest(
		t,
		sh,
		http.MethodPost,
		"/_shamhub/admin/repos/fork",
		adminForkRepositoryBody{
			Owner:     "alice",
			Repo:      "example",
			ForkOwner: "bob",
		},
		&fork,
	)
	assert.Equal(t, sh.RepoURL("bob", "example"), fork.URL)

	adminRequest(
		t,
		sh,
		http.MethodPost,
		"/_shamhub/admin/config",
		adminConfigBody{
			Key:   "mergeMethod",
			Value: "squash",
		},
		&adminConfigResponse{},
	)
	assert.Equal(t, MergeMethodSquash, sh.defaultMergeMethod)
}

func TestAdminChangeControls(t *testing.T) {
	sh, err := New(Config{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, sh.Close())
	})
	seedMergeabilityChange(sh)

	adminRequest(t, sh, http.MethodPost,
		"/_shamhub/admin/changes/alice/example/1/checks",
		adminSetStatusBody{Name: "ci", State: "passed"},
		&adminSetStatusResponse{},
	)
	checks, err := sh.ChangeChecks("alice", "example", 1)
	require.NoError(t, err)
	assert.Equal(t, []forge.ChangeCheck{{
		Name:  "ci",
		State: forge.ChangeCheckPassed,
	}}, checks)

	adminRequest(t, sh, http.MethodPost,
		"/_shamhub/admin/changes/alice/example/1/mergeability",
		adminSetMergeabilityBody{State: "blocked", Reason: "checks"},
		&adminSetMergeabilityResponse{},
	)
	mergeability, err := sh.ChangeMergeability("alice", "example", 1)
	require.NoError(t, err)
	assert.Equal(t, forge.ChangeMergeability{
		State:  forge.ChangeMergeabilityBlocked,
		Reason: forge.ChangeMergeabilityReasonChecks,
	}, mergeability)

	adminRequest(t, sh, http.MethodPost,
		"/_shamhub/admin/changes/alice/example/1/reject",
		adminRejectChangeBody{},
		&adminRejectChangeResponse{},
	)
	assert.Equal(t, shamChangeClosed, sh.changes[0].State)
}

func TestAdminMergeChange(t *testing.T) {
	sh, repo := newMergeabilityTestRepository(t)

	workDir := t.TempDir()
	worktree, err := git.Clone(
		t.Context(),
		sh.RepoURL("alice", "example"),
		workDir,
		git.CloneOptions{Log: silogtest.New(t)},
	)
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

	require.NoError(t, worktree.Repository().CreateBranch(
		t.Context(),
		git.CreateBranchRequest{Name: "feature", Head: "HEAD"},
	))
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
	var changes adminDumpChangesResponse
	adminRequest(t, sh, http.MethodGet,
		"/_shamhub/admin/dump/changes",
		nil,
		&changes,
	)
	require.Len(t, changes.Changes, 1)
	assert.Equal(t, 1, changes.Changes[0].Number)

	var change adminDumpChangeResponse
	adminRequest(t, sh, http.MethodGet,
		"/_shamhub/admin/dump/changes/1",
		nil,
		&change,
	)
	assert.Equal(t, 1, change.Change.Number)

	adminRequest(t, sh, http.MethodPost,
		"/_shamhub/admin/changes/alice/example/1/merge",
		adminMergeChangeBody{},
		&adminMergeChangeResponse{},
	)
	assert.Equal(t, shamChangeMerged, sh.changes[0].State)
}

func TestAdminCommentsAndDumps(t *testing.T) {
	sh, err := New(Config{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, sh.Close())
	})
	seedMergeabilityChange(sh)

	var comment adminPostCommentResponse
	adminRequest(
		t,
		sh,
		http.MethodPost,
		"/_shamhub/admin/comments",
		adminPostCommentBody{
			Owner:      "alice",
			Repo:       "example",
			Change:     1,
			ID:         101,
			Body:       "needs work",
			Resolvable: true,
		},
		&comment,
	)
	assert.Equal(t, 101, comment.ID)

	resolved := true
	adminRequest(
		t,
		sh,
		http.MethodPatch,
		"/_shamhub/admin/comments/101",
		adminEditCommentBody{Resolved: &resolved},
		&adminEditCommentResponse{},
	)

	var comments adminDumpCommentsResponse
	adminRequest(t, sh, http.MethodGet,
		"/_shamhub/admin/dump/comments?change=1",
		nil,
		&comments,
	)
	require.Len(t, comments.Comments, 1)
	assert.Equal(t, "needs work", comments.Comments[0].Body)

	adminRequest(t, sh, http.MethodDelete,
		"/_shamhub/admin/comments/101",
		nil,
		&adminDeleteCommentResponse{},
	)
	allComments, err := sh.ListChangeComments()
	require.NoError(t, err)
	assert.Empty(t, allComments)
}

func adminStatus(
	t *testing.T,
	sh *ShamHub,
	token string,
	method string,
	path string,
	req any,
) int {
	t.Helper()

	body, err := json.Marshal(req)
	require.NoError(t, err)

	httpReq, err := http.NewRequestWithContext(
		t.Context(),
		method,
		sh.APIURL()+path,
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	if token != "" {
		httpReq.Header.Set("ShamHub-Admin-Token", token)
	}

	httpResp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, httpResp.Body.Close())
	}()

	return httpResp.StatusCode
}

func adminRequest(
	t *testing.T,
	sh *ShamHub,
	method string,
	path string,
	req any,
	res any,
) {
	t.Helper()

	var body bytes.Buffer
	if req != nil {
		require.NoError(t, json.NewEncoder(&body).Encode(req))
	}

	httpReq, err := http.NewRequestWithContext(
		t.Context(),
		method,
		sh.APIURL()+path,
		&body,
	)
	require.NoError(t, err)
	httpReq.Header.Set("ShamHub-Admin-Token", sh.AdminToken())

	httpResp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, httpResp.Body.Close())
	}()
	require.Equal(t, http.StatusOK, httpResp.StatusCode)

	if res != nil {
		require.NoError(t, json.NewDecoder(httpResp.Body).Decode(res))
	}
}
