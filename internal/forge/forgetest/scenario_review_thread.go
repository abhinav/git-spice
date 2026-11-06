package forgetest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/fixturetest"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/sliceutil"
)

func (s *integrationSuite) TestReviewThreads(t *testing.T) {
	t.Run("SingleAndReply", s.TestReviewThreadsSingleAndReply)
	t.Run("Batch", s.TestReviewThreadsBatch)
	if s.fileReviewThreads {
		t.Run("File", s.TestReviewThreadsFile)
	}
	t.Run("ReviewerStates", s.TestReviewThreadsReviewerStates)
}

func (s *integrationSuite) TestReviewThreadsSingleAndReply(t *testing.T) {
	rootCommitHash, setRootCommitHash := fixturetest.Stored[string](
		s.Fixtures, "rootCommitHash",
	)
	replyHeadHash, setReplyHeadHash := fixturetest.Stored[string](
		s.Fixtures, "replyHeadHash",
	)
	branchName := fixturetest.New(s.Fixtures, "branch", func() string {
		return randomString(8)
	}).Get(t)

	// Update mode creates the Git state consumed by the live forge.
	// Replay mode reuses the recorded branch name and HTTP interactions.
	if Update() {
		testRepo := NewRepositoryBuilder(t, s.RemoteURL)
		testRepo.CreateBranch(branchName)
		testRepo.CheckoutBranch(branchName)
		testRepo.WriteFile(
			"review.go",
			"package review",
			"",
			"func value() int {",
			"\treturn 1",
			"}",
		)
		rootHash := testRepo.AddAllAndCommit("commit for review comments")
		testRepo.Push(branchName)
		if s.reviewThreadCommitHash {
			setRootCommitHash(rootHash.String())
		}

		t.Cleanup(func() {
			testRepo.DeleteRemoteBranch(branchName)
		})
	}

	repo := s.OpenRepository(t)
	reviewRepo, ok := repo.(forge.ReviewRepository)
	require.True(t, ok, "repository does not implement ReviewRepository")

	change, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Testing review threads " + branchName,
		Body:    "Test change for review threads",
		Base:    "main",
		Head:    branchName,
	})
	require.NoError(t, err, "submit change")
	if Update() {
		t.Cleanup(func() {
			s.CloseChange(t, repo, change.ID)
		})

		// Some forges create the change before its diff is ready.
		// Give asynchronous processing time to expose review positions.
		time.Sleep(5 * time.Second)
	}

	// A reply must preserve the thread identity returned for its root comment.
	singleReview, err := reviewRepo.SubmitReview(
		t.Context(),
		change.ID,
		forge.SubmitReviewRequest{
			Comments: []forge.SubmitReviewCommentRequest{
				{
					Path:  "review.go",
					Range: forge.ReviewThreadLine(3),
					Body:  "Single-line review comment.",
					Side:  forge.ReviewThreadSideRight,
				},
			},
		},
	)
	require.NoError(t, err, "submit review comment")
	require.Len(t, singleReview.Comments, 1)
	single := singleReview.Comments[0]
	require.NotNil(t, single.ThreadID)
	require.NotNil(t, single.CommentID)
	if s.reviewThreadCommitHash {
		threads, err := sliceutil.CollectErr(
			reviewRepo.ListReviewThreads(t.Context(), change.ID))
		require.NoError(t, err, "list review threads after root creation")
		var rootThread *forge.ReviewThread
		for _, thread := range threads {
			for _, comment := range thread.Comments {
				if comment.Body == "Single-line review comment." {
					rootThread = thread
				}
			}
		}
		require.NotNil(t, rootThread, "single review thread not found")
		wantRootHash := rootCommitHash.Get(t)
		t.Run("RootCommitHash", func(t *testing.T) {
			assert.Equal(t, wantRootHash, rootThread.CommitHash.String())
		})
	}

	if s.reviewThreadCommitHash && Update() {
		testRepo := NewRepositoryBuilder(t, s.RemoteURL)
		testRepo.CheckoutBranch(branchName)
		testRepo.WriteFile("reply.go", "package review")
		replyHash := testRepo.AddAllAndCommit("advance change before reply")
		testRepo.Push(branchName)
		setReplyHeadHash(replyHash.String())
	}

	replyReview, err := reviewRepo.SubmitReview(
		t.Context(),
		change.ID,
		forge.SubmitReviewRequest{
			Comments: []forge.SubmitReviewCommentRequest{
				{
					ReplyTo: single.ThreadID,
					Body:    "Reply review comment.",
				},
			},
		},
	)
	require.NoError(t, err, "reply to review thread")
	require.Len(t, replyReview.Comments, 1)
	reply := replyReview.Comments[0]
	assert.Equal(t, single.ThreadID, reply.ThreadID)
	require.NotNil(t, reply.CommentID)

	// Listing verifies that submission identifiers remain stable
	// after the forge stores and reconstructs the thread.
	threads, err := sliceutil.CollectErr(
		reviewRepo.ListReviewThreads(t.Context(), change.ID))
	require.NoError(t, err, "list review threads")
	var (
		singleThread  *forge.ReviewThread
		singleComment *forge.ReviewComment
		replyThread   *forge.ReviewThread
	)
	for _, thread := range threads {
		for i := range thread.Comments {
			comment := &thread.Comments[i]
			switch comment.Body {
			case "Single-line review comment.":
				singleThread = thread
				singleComment = comment
			case "Reply review comment.":
				replyThread = thread
			}
		}
	}
	require.NotNil(t, singleThread, "single review thread not found")
	require.NotNil(t, singleComment, "single review comment not found")
	require.NotNil(t, replyThread, "reply review thread not found")
	assert.Equal(t, single.ThreadID, singleThread.ID)
	assert.Equal(t, single.CommentID, singleComment.ID)
	assert.Equal(t, single.ThreadID, replyThread.ID)
	assert.Equal(t, "review.go", singleThread.Path)
	assert.Equal(t, forge.ReviewThreadLine(3), singleThread.Range)

	if s.reviewThreadCommitHash {
		wantRootHash := rootCommitHash.Get(t)
		wantReplyHeadHash := replyHeadHash.Get(t)
		t.Run("ReplyRetainsRootCommitHash", func(t *testing.T) {
			assert.NotEqual(t, wantRootHash, wantReplyHeadHash)
			assert.Equal(t, wantRootHash, replyThread.CommitHash.String())
		})
	}

	// Editing and resolution are independent optional upgrades.
	// Exercise each one only when the repository advertises it.
	if editor, ok := repo.(forge.ReviewCommentEditor); ok {
		t.Run("UpdateComment", func(t *testing.T) {
			const editedBody = "Edited single-line review comment."
			require.NoError(t, editor.UpdateReviewComment(
				t.Context(), single.CommentID, editedBody,
			), "update review comment")
			threads, err := sliceutil.CollectErr(
				reviewRepo.ListReviewThreads(t.Context(), change.ID))
			require.NoError(t, err, "list review threads")

			var foundEditedComment bool
			for _, thread := range threads {
				for _, comment := range thread.Comments {
					if comment.Body == editedBody {
						foundEditedComment = true
					}
				}
			}
			assert.True(t, foundEditedComment, "edited comment not found")
		})
	}

	if resolver, ok := repo.(forge.ReviewThreadResolver); ok {
		t.Run("Resolution", func(t *testing.T) {
			require.NoError(t, resolver.ResolveReviewThread(
				t.Context(), single.ThreadID,
			), "resolve review thread")
			threads, err := sliceutil.CollectErr(
				reviewRepo.ListReviewThreads(t.Context(), change.ID))
			require.NoError(t, err, "list review threads")

			var resolvedThread *forge.ReviewThread
			for _, thread := range threads {
				if thread.ID != nil &&
					thread.ID.String() == single.ThreadID.String() {
					resolvedThread = thread
				}
			}
			require.NotNil(t, resolvedThread, "review thread not found")
			require.NotNil(t, resolvedThread.Resolved,
				"resolution state is not exposed")
			assert.True(t, *resolvedThread.Resolved)

			require.NoError(t, resolver.UnresolveReviewThread(
				t.Context(), single.ThreadID,
			), "unresolve review thread")
			threads, err = sliceutil.CollectErr(
				reviewRepo.ListReviewThreads(t.Context(), change.ID))
			require.NoError(t, err, "list review threads")

			resolvedThread = nil
			for _, thread := range threads {
				if thread.ID != nil &&
					thread.ID.String() == single.ThreadID.String() {
					resolvedThread = thread
				}
			}
			require.NotNil(t, resolvedThread, "review thread not found")
			require.NotNil(t, resolvedThread.Resolved,
				"resolution state is not exposed")
			assert.False(t, *resolvedThread.Resolved)
		})
	}
}

func (s *integrationSuite) TestReviewThreadsBatch(t *testing.T) {
	branchName := fixturetest.New(s.Fixtures, "branch", func() string {
		return randomString(8)
	}).Get(t)

	// Update mode creates the Git state consumed by the live forge.
	// Replay mode reuses the recorded branch name and HTTP interactions.
	if Update() {
		testRepo := NewRepositoryBuilder(t, s.RemoteURL)
		testRepo.CreateBranch(branchName)
		testRepo.CheckoutBranch(branchName)
		testRepo.WriteFile(
			"review.go",
			"package review",
			"",
			"func value() int {",
			"\treturn 1",
			"}",
		)
		testRepo.AddAllAndCommit("commit for review comments")
		testRepo.Push(branchName)

		t.Cleanup(func() {
			testRepo.DeleteRemoteBranch(branchName)
		})
	}

	repo := s.OpenRepository(t)
	reviewRepo, ok := repo.(forge.ReviewRepository)
	require.True(t, ok, "repository does not implement ReviewRepository")

	change, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Testing review threads " + branchName,
		Body:    "Test change for review threads",
		Base:    "main",
		Head:    branchName,
	})
	require.NoError(t, err, "submit change")
	if Update() {
		t.Cleanup(func() {
			s.CloseChange(t, repo, change.ID)
		})

		// Some forges create the change before its diff is ready.
		// Give asynchronous processing time to expose review positions.
		time.Sleep(5 * time.Second)
	}

	// Two comments ensure this exercises one multi-comment submission
	// rather than the single-comment path.
	batchReview, err := reviewRepo.SubmitReview(
		t.Context(),
		change.ID,
		forge.SubmitReviewRequest{
			Comments: []forge.SubmitReviewCommentRequest{
				{
					Path:  "review.go",
					Range: forge.ReviewThreadLine(3),
					Body:  "First batched review comment.",
					Side:  forge.ReviewThreadSideRight,
				},
				{
					Path:  "review.go",
					Range: forge.ReviewThreadLine(4),
					Body:  "Second batched review comment.",
					Side:  forge.ReviewThreadSideRight,
				},
			},
		},
	)
	require.NoError(t, err, "submit review")
	require.Len(t, batchReview.Comments, 2)
	for _, comment := range batchReview.Comments {
		require.NotNil(t, comment.ThreadID)
		require.NotNil(t, comment.CommentID)
	}

	// Match each listed body to its positional submission result.
	wantComments := map[string]forge.SubmitReviewCommentResult{
		"First batched review comment.":  batchReview.Comments[0],
		"Second batched review comment.": batchReview.Comments[1],
	}
	threads, err := sliceutil.CollectErr(
		reviewRepo.ListReviewThreads(t.Context(), change.ID))
	require.NoError(t, err, "list review threads")
	for _, thread := range threads {
		for _, comment := range thread.Comments {
			want, ok := wantComments[comment.Body]
			if !ok {
				continue
			}
			assert.Equal(t, want.ThreadID, thread.ID)
			assert.Equal(t, want.CommentID, comment.ID)
			delete(wantComments, comment.Body)
		}
	}
	assert.Empty(t, wantComments, "batched review comments not found")
}

func (s *integrationSuite) TestReviewThreadsFile(t *testing.T) {
	branchName := fixturetest.New(s.Fixtures, "branch", func() string {
		return randomString(8)
	}).Get(t)

	// Update mode creates the Git state consumed by the live forge.
	// Replay mode reuses the recorded branch name and HTTP interactions.
	if Update() {
		testRepo := NewRepositoryBuilder(t, s.RemoteURL)
		testRepo.CreateBranch(branchName)
		testRepo.CheckoutBranch(branchName)
		testRepo.WriteFile(
			"review.go",
			"package review",
			"",
			"func value() int {",
			"\treturn 1",
			"}",
		)
		testRepo.AddAllAndCommit("commit for file review comment")
		testRepo.Push(branchName)

		t.Cleanup(func() {
			testRepo.DeleteRemoteBranch(branchName)
		})
	}

	repo := s.OpenRepository(t)
	reviewRepo, ok := repo.(forge.ReviewRepository)
	require.True(t, ok, "repository does not implement ReviewRepository")

	change, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Testing file review thread " + branchName,
		Body:    "Test change for a file-level review thread",
		Base:    "main",
		Head:    branchName,
	})
	require.NoError(t, err, "submit change")
	if Update() {
		t.Cleanup(func() {
			s.CloseChange(t, repo, change.ID)
		})

		// Some forges create the change before its diff is ready.
		// Give asynchronous processing time to expose review positions.
		time.Sleep(5 * time.Second)
	}

	result, err := reviewRepo.SubmitReview(
		t.Context(),
		change.ID,
		forge.SubmitReviewRequest{
			Comments: []forge.SubmitReviewCommentRequest{
				{
					Path: "review.go",
					Body: "File-level review comment.",
				},
			},
		},
	)
	require.NoError(t, err, "submit file-level review comment")
	require.Len(t, result.Comments, 1)
	require.NotNil(t, result.Comments[0].ThreadID)
	require.NotNil(t, result.Comments[0].CommentID)

	// The zero range must survive the forge's native representation
	// rather than being reconstructed as an arbitrary line anchor.
	threads, err := sliceutil.CollectErr(
		reviewRepo.ListReviewThreads(t.Context(), change.ID))
	require.NoError(t, err, "list review threads")
	var (
		fileThread  *forge.ReviewThread
		fileComment *forge.ReviewComment
	)
	for _, thread := range threads {
		for i := range thread.Comments {
			comment := &thread.Comments[i]
			if comment.Body == "File-level review comment." {
				fileThread = thread
				fileComment = comment
			}
		}
	}
	require.NotNil(t, fileThread, "file-level review thread not found")
	require.NotNil(t, fileComment, "file-level review comment not found")
	assert.Equal(t, result.Comments[0].ThreadID, fileThread.ID)
	assert.Equal(t, result.Comments[0].CommentID, fileComment.ID)
	assert.Equal(t, "review.go", fileThread.Path)
	assert.True(t, fileThread.Range.IsZero())
}

func (s *integrationSuite) TestReviewThreadsReviewerStates(t *testing.T) {
	branchName := fixturetest.New(s.Fixtures, "branch", func() string {
		return randomString(8)
	}).Get(t)

	// Update mode creates the Git state consumed by the live forge.
	// Replay mode reuses the recorded branch name and HTTP interactions.
	if Update() {
		testRepo := NewRepositoryBuilder(t, s.RemoteURL)
		testRepo.CreateBranch(branchName)
		testRepo.CheckoutBranch(branchName)
		testRepo.WriteFile(
			"review.go",
			"package review",
			"",
			"func value() int {",
			"\treturn 1",
			"}",
		)
		testRepo.AddAllAndCommit("commit for review comments")
		testRepo.Push(branchName)

		t.Cleanup(func() {
			testRepo.DeleteRemoteBranch(branchName)
		})
	}

	repo := s.OpenRepository(t)
	reviewRepo, ok := repo.(forge.ReviewRepository)
	require.True(t, ok, "repository does not implement ReviewRepository")

	change, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Testing review threads " + branchName,
		Body:    "Test change for review threads",
		Base:    "main",
		Head:    branchName,
	})
	require.NoError(t, err, "submit change")
	if Update() {
		t.Cleanup(func() {
			s.CloseChange(t, repo, change.ID)
		})

		// Some forges create the change before its diff is ready.
		// Give asynchronous processing time to expose review positions.
		time.Sleep(5 * time.Second)
	}

	// Comments and review disposition are independent parts of the request.
	// Posting comments without a disposition must not create reviewer state.
	_, err = reviewRepo.SubmitReview(
		t.Context(),
		change.ID,
		forge.SubmitReviewRequest{
			Comments: []forge.SubmitReviewCommentRequest{
				{
					Path:  "review.go",
					Range: forge.ReviewThreadLine(3),
					Body:  "Comment without review disposition.",
					Side:  forge.ReviewThreadSideRight,
				},
			},
		},
	)
	require.NoError(t, err, "submit comment without review disposition")

	states, err := sliceutil.CollectErr(
		reviewRepo.ListReviewerStates(t.Context(), change.ID))
	require.NoError(t, err, "list reviewer states")
	assert.Empty(t, states, "comment submission created reviewer state")
}
