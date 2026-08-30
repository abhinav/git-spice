package shamhub

import (
	json "encoding/json/v2"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
)

func TestForgeRepository_SubmitReview_invalidEnums(t *testing.T) {
	sh, repo := newMergeabilityTestRepository(t)
	seedMergeabilityChange(sh)

	t.Run("Disposition", func(t *testing.T) {
		assert.Panics(t, func() {
			_, _ = repo.SubmitReview(
				t.Context(),
				ChangeID(1),
				forge.SubmitReviewRequest{
					Body:        "Review body.",
					Disposition: forge.ReviewDisposition(99),
				},
			)
		})
	})

	t.Run("ThreadSide", func(t *testing.T) {
		assert.Panics(t, func() {
			_, _ = repo.SubmitReview(
				t.Context(),
				ChangeID(1),
				forge.SubmitReviewRequest{
					Comments: []forge.SubmitReviewCommentRequest{
						{
							Path:  "review.go",
							Range: forge.ReviewThreadLine(3),
							Side:  forge.ReviewThreadSide(99),
							Body:  "Review comment.",
						},
					},
				},
			)
		})
	})
}

func TestForgeRepository_SubmitReview_fileReviewThread(t *testing.T) {
	sh, repo := newMergeabilityTestRepository(t)
	seedMergeabilityChange(sh)
	sh.changes[0].HeadHash = "1111111111111111111111111111111111111111"

	var (
		result forge.SubmitReviewResult
		err    error
	)
	require.NotPanics(t, func() {
		result, err = repo.SubmitReview(
			t.Context(),
			ChangeID(1),
			forge.SubmitReviewRequest{
				Comments: []forge.SubmitReviewCommentRequest{
					{
						Path: "review.go",
						Side: forge.ReviewThreadSide(99),
						Body: "File-level comment.",
					},
				},
			},
		)
	})
	require.NoError(t, err)
	require.Len(t, result.Comments, 1)
	require.Len(t, sh.comments, 1)
	assert.Equal(t, "review.go", sh.comments[0].Path)
	assert.Zero(t, sh.comments[0].Line)
	assert.Zero(t, sh.comments[0].RangeStart)
	assert.Zero(t, sh.comments[0].RangeEnd)
	assert.Zero(t, sh.comments[0].Side)

	_, err = repo.SubmitReview(
		t.Context(),
		ChangeID(1),
		forge.SubmitReviewRequest{
			Comments: []forge.SubmitReviewCommentRequest{
				{
					ReplyTo: result.Comments[0].ThreadID,
					Body:    "Reply.",
				},
			},
		},
	)
	require.NoError(t, err)
	require.Len(t, sh.comments, 2)
	assert.Empty(t, sh.comments[1].Path)
	assert.Zero(t, sh.comments[1].Line)
	assert.Zero(t, sh.comments[1].RangeStart)
	assert.Zero(t, sh.comments[1].RangeEnd)
	assert.Zero(t, sh.comments[1].Side)

	u := repo.apiURL.JoinPath(repo.owner, repo.repo, "review-threads")
	query := u.Query()
	query.Set("change", "1")
	u.RawQuery = query.Encode()
	httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, u.String(), nil)
	require.NoError(t, err)
	for key, value := range repo.client.headers {
		httpReq.Header.Set(key, value)
	}
	httpRes, err := repo.client.client.Do(httpReq)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, httpRes.Body.Close())
	}()
	responseBody, err := io.ReadAll(httpRes.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, httpRes.StatusCode)
	var response struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(responseBody, &response))
	require.Len(t, response.Items, 2)
	root := response.Items[0]
	assert.Equal(t, "File-level comment.", root["body"])
	for _, key := range []string{"line", "rangeStart", "rangeEnd", "side"} {
		assert.NotContains(t, root, key)
	}

	var threads []*forge.ReviewThread
	for thread, err := range repo.ListReviewThreads(t.Context(), ChangeID(1)) {
		require.NoError(t, err)
		threads = append(threads, thread)
	}
	require.Len(t, threads, 1)
	assert.Equal(t, result.Comments[0].ThreadID, threads[0].ID)
	assert.Equal(t, "review.go", threads[0].Path)
	assert.True(t, threads[0].Range.IsZero())
	require.Len(t, threads[0].Comments, 2)
	assert.Equal(t, "File-level comment.", threads[0].Comments[0].Body)
	assert.Equal(t, "Reply.", threads[0].Comments[1].Body)
}

func TestShamHub_FeedbackSubmissionStorage(t *testing.T) {
	sh := &ShamHub{
		changes: []shamChange{
			{
				Number:   1,
				HeadHash: "1111111111111111111111111111111111111111",
				Base: &shamBranch{
					Owner: "alice",
					Repo:  "example",
				},
			},
		},
	}
	ctx := contextWithShamHubUser(t.Context(), "reviewer")

	result, err := sh.handleSubmitReview(ctx, &submitReviewRequest{
		Owner:       "alice",
		Repo:        "example",
		Change:      1,
		Body:        "Overall review.",
		Disposition: int(forge.ReviewDispositionApprove),
		Comments: []submitReviewCommentRequest{
			{
				Path:       "review.go",
				Line:       3,
				RangeStart: 3,
				RangeEnd:   5,
				Side:       int(forge.ReviewThreadSideRight),
				Body:       "Range comment.",
			},
			{
				Path: "review.go",
				Line: 8,
				Side: int(forge.ReviewThreadSideLeft),
				Body: "Left-side comment.",
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Comments, 2)
	assert.Equal(t, "thread-2", result.Comments[0].ThreadID)
	assert.Equal(t, 2, result.Comments[0].CommentID)
	assert.Equal(t, "thread-3", result.Comments[1].ThreadID)
	assert.Equal(t, 3, result.Comments[1].CommentID)
	require.Len(t, sh.feedbackSubmissions, 1)
	assert.Equal(t, []int{1, 2, 3}, sh.feedbackSubmissions[0].CommentIDs)
	assert.Equal(t, "1111111111111111111111111111111111111111", sh.comments[1].CommitHash.String())

	sh.changes[0].HeadHash = "2222222222222222222222222222222222222222"

	reply, err := sh.handleSubmitReview(ctx, &submitReviewRequest{
		Owner:       "alice",
		Repo:        "example",
		Change:      1,
		Disposition: int(forge.ReviewDispositionRequestChanges),
		Comments: []submitReviewCommentRequest{
			{
				ThreadID: "thread-2",
				Body:     "Reply comment.",
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, reply.Comments, 1)
	assert.Equal(t, "thread-2", reply.Comments[0].ThreadID)
	assert.Equal(t, 4, reply.Comments[0].CommentID)
	assert.Equal(t, "1111111111111111111111111111111111111111", sh.comments[3].CommitHash.String())
	sh.changes[0].HeadHash = "1111111111111111111111111111111111111111"

	threads, err := sh.handleListReviewThreads(t.Context(), &listReviewThreadsRequest{
		Owner:  "alice",
		Repo:   "example",
		Change: 1,
	})
	require.NoError(t, err)
	require.Len(t, threads.Items, 3)
	assert.Equal(t, "review.go", threads.Items[0].Path)
	assert.Equal(t, 3, threads.Items[0].Line)
	assert.Equal(t, 3, threads.Items[0].RangeStart)
	assert.Equal(t, 5, threads.Items[0].RangeEnd)
	assert.Equal(t, int(forge.ReviewThreadSideRight), threads.Items[0].Side)
	assert.Equal(t, "reviewer", threads.Items[0].Author)
	assert.Equal(t, int(forge.ReviewThreadSideLeft), threads.Items[1].Side)
	assert.Empty(t, threads.Items[2].Path)
	assert.Equal(t, "thread-2", threads.Items[2].ThreadID)
	assert.Equal(t, "1111111111111111111111111111111111111111", threads.Items[2].CommitHash)

	reviews, err := sh.handleListReviewerStates(t.Context(), &listReviewerStatesRequest{
		Owner:  "alice",
		Repo:   "example",
		Change: 1,
	})
	require.NoError(t, err)
	require.Len(t, reviews.States, 1)
	assert.Equal(t, "reviewer", reviews.States[0].Reviewer)
	assert.Equal(t,
		int(forge.ReviewDispositionRequestChanges),
		reviews.States[0].Disposition)
	assert.False(t, reviews.States[0].SubmittedAt.IsZero())

	_, err = sh.handleResolveReviewThread(t.Context(), &reviewThreadStateRequest{
		Owner:    "alice",
		Repo:     "example",
		ThreadID: "thread-2",
	})
	require.NoError(t, err)

	threads, err = sh.handleListReviewThreads(t.Context(), &listReviewThreadsRequest{
		Owner:  "alice",
		Repo:   "example",
		Change: 1,
	})
	require.NoError(t, err)
	assert.True(t, threads.Items[0].Resolved)
	assert.True(t, threads.Items[2].Resolved)

	comments, err := sh.ListChangeComments()
	require.NoError(t, err)
	require.Len(t, comments, 1)
	assert.Equal(t, "Overall review.", comments[0].Body)
}

func TestShamHub_ReviewerStatesIgnoreCommentSubmissions(t *testing.T) {
	sh := &ShamHub{
		changes: []shamChange{
			{
				Number: 1,
				Base: &shamBranch{
					Owner: "alice",
					Repo:  "example",
				},
			},
		},
	}
	ctx := contextWithShamHubUser(t.Context(), "reviewer")

	_, err := sh.handleSubmitReview(ctx, &submitReviewRequest{
		Owner:       "alice",
		Repo:        "example",
		Change:      1,
		Body:        "Comment submission.",
		Disposition: int(forge.ReviewDispositionNone),
	})
	require.NoError(t, err)
	states, err := sh.handleListReviewerStates(t.Context(), &listReviewerStatesRequest{
		Owner:  "alice",
		Repo:   "example",
		Change: 1,
	})
	require.NoError(t, err)
	assert.Empty(t, states.States)

	_, err = sh.handleSubmitReview(ctx, &submitReviewRequest{
		Owner:       "alice",
		Repo:        "example",
		Change:      1,
		Body:        "Approved.",
		Disposition: int(forge.ReviewDispositionApprove),
	})
	require.NoError(t, err)
	states, err = sh.handleListReviewerStates(t.Context(), &listReviewerStatesRequest{
		Owner:  "alice",
		Repo:   "example",
		Change: 1,
	})
	require.NoError(t, err)
	require.Len(t, states.States, 1)
	assert.Equal(t, "reviewer", states.States[0].Reviewer)
	assert.Equal(t, int(forge.ReviewDispositionApprove), states.States[0].Disposition)

	_, err = sh.handleSubmitReview(ctx, &submitReviewRequest{
		Owner:       "alice",
		Repo:        "example",
		Change:      1,
		Body:        "Follow-up comment.",
		Disposition: int(forge.ReviewDispositionNone),
	})
	require.NoError(t, err)
	states, err = sh.handleListReviewerStates(t.Context(), &listReviewerStatesRequest{
		Owner:  "alice",
		Repo:   "example",
		Change: 1,
	})
	require.NoError(t, err)
	require.Len(t, states.States, 1)
	assert.Equal(t, "reviewer", states.States[0].Reviewer)
	assert.Equal(t, int(forge.ReviewDispositionApprove), states.States[0].Disposition)

	require.Len(t, sh.feedbackSubmissions, 3)
	assert.Equal(t, forge.ReviewDispositionNone, sh.feedbackSubmissions[0].Disposition)
	assert.Equal(t, forge.ReviewDispositionApprove, sh.feedbackSubmissions[1].Disposition)
	assert.Equal(t, forge.ReviewDispositionNone, sh.feedbackSubmissions[2].Disposition)
}

func TestShamHub_ReviewCommentDomain(t *testing.T) {
	sh, repo := newMergeabilityTestRepository(t)
	seedMergeabilityChange(sh)
	sh.changes[0].HeadHash = "1111111111111111111111111111111111111111"

	result, err := repo.SubmitReview(t.Context(), ChangeID(1), forge.SubmitReviewRequest{
		Body: "Overall review.",
		Comments: []forge.SubmitReviewCommentRequest{
			{
				Path:  "review.go",
				Range: forge.ReviewThreadLine(3),
				Body:  "Thread comment.",
				Side:  forge.ReviewThreadSideRight,
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Comments, 1)
	assert.IsType(t, ReviewCommentID(0), result.Comments[0].CommentID)

	require.NoError(t, repo.UpdateReviewComment(
		t.Context(), result.Comments[0].CommentID, "Edited thread comment.",
	))

	var listedComments []forge.ReviewComment
	for thread, err := range repo.ListReviewThreads(t.Context(), ChangeID(1)) {
		require.NoError(t, err)
		listedComments = append(listedComments, thread.Comments...)
	}
	require.Len(t, listedComments, 1)
	assert.IsType(t, ReviewCommentID(0), listedComments[0].ID)
	assert.Equal(t, "Edited thread comment.", listedComments[0].Body)

	var changeComments []*forge.ListChangeCommentItem
	for comment, err := range repo.ListChangeComments(t.Context(), ChangeID(1), nil) {
		require.NoError(t, err)
		changeComments = append(changeComments, comment)
	}
	require.Len(t, changeComments, 1)
	assert.Equal(t, "Overall review.", changeComments[0].Body)
}

func TestShamHub_SubmitReviewHTTPValidation(t *testing.T) {
	sh, repo := newMergeabilityTestRepository(t)
	seedMergeabilityChange(sh)
	sh.changes = append(sh.changes, shamChange{
		Number: 2,
		Base: &shamBranch{
			Owner: "alice",
			Repo:  "example",
		},
	})
	sh.comments = append(sh.comments, shamComment{
		ID:         1,
		Change:     2,
		Body:       "Other change.",
		Resolvable: true,
		ThreadID:   "thread-1",
	})

	tests := []struct {
		name string
		req  submitReviewRequest
		want string
	}{
		{
			name: "EmptyReview",
			req: submitReviewRequest{
				Owner:  "alice",
				Repo:   "example",
				Change: 1,
			},
			want: "submission must include",
		},
		{
			name: "EmptyCommentBody",
			req: submitReviewRequest{
				Owner:  "alice",
				Repo:   "example",
				Change: 1,
				Comments: []submitReviewCommentRequest{
					{
						Path: "review.go",
						Line: 3,
					},
				},
			},
			want: "comment 0 body is required",
		},
		{
			name: "InvalidDisposition",
			req: submitReviewRequest{
				Change:      1,
				Body:        "Review body.",
				Disposition: 99,
			},
			want: "invalid review disposition",
		},
		{
			name: "MissingPath",
			req: submitReviewRequest{
				Change: 1,
				Comments: []submitReviewCommentRequest{
					{
						Line: 3,
						Side: int(forge.ReviewThreadSideRight),
						Body: "Missing path.",
					},
				},
			},
			want: "comment 0 path is required",
		},
		{
			name: "InvalidRange",
			req: submitReviewRequest{
				Owner:  "alice",
				Repo:   "example",
				Change: 1,
				Comments: []submitReviewCommentRequest{
					{
						Path:       "review.go",
						Line:       5,
						RangeStart: 5,
						RangeEnd:   3,
						Body:       "Invalid range.",
					},
				},
			},
			want: "invalid review range",
		},
		{
			name: "InvalidSide",
			req: submitReviewRequest{
				Change: 1,
				Comments: []submitReviewCommentRequest{
					{
						Path: "review.go",
						Line: 3,
						Side: 99,
						Body: "Invalid side.",
					},
				},
			},
			want: "invalid review side",
		},
		{
			name: "UnknownReply",
			req: submitReviewRequest{
				Owner:  "alice",
				Repo:   "example",
				Change: 1,
				Comments: []submitReviewCommentRequest{
					{
						ThreadID: "thread-99",
						Body:     "Unknown reply.",
					},
				},
			},
			want: "thread \"thread-99\" not found",
		},
		{
			name: "ReplyOnOtherChange",
			req: submitReviewRequest{
				Owner:  "alice",
				Repo:   "example",
				Change: 1,
				Comments: []submitReviewCommentRequest{
					{
						ThreadID: "thread-1",
						Body:     "Wrong change.",
					},
				},
			},
			want: "does not belong to change 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := repo.apiURL.JoinPath(repo.owner, repo.repo, "reviews")
			var response submitReviewResponse
			err := repo.client.Post(t.Context(), u.String(), tt.req, &response)
			require.Error(t, err)
			assert.ErrorContains(t, err, "unexpected status code: 400")
			assert.ErrorContains(t, err, tt.want)
		})
	}
}
