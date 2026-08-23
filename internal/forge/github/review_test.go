package github

import (
	"errors"
	"iter"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.uber.org/mock/gomock"
)

func TestRepository_ListReviewerStates(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	submittedAt := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	approved := &github.PullRequestLatestOpinionatedReview{
		Author: github.ReviewAuthor{Login: "approved"},
		State:  github.ReviewStateApproved, SubmittedAt: submittedAt,
	}
	approved.Commit.OID = "1111111111111111111111111111111111111111"
	gateway.EXPECT().PullRequestLatestOpinionatedReviews(gomock.Any(), github.ID("PR_1"), nil).Return(reviewSeq(
		approved,
		&github.PullRequestLatestOpinionatedReview{Author: github.ReviewAuthor{Login: "commented"}, State: github.ReviewStateCommented},
		&github.PullRequestLatestOpinionatedReview{Author: github.ReviewAuthor{Login: "changes"}, State: github.ReviewStateChangesRequested},
		&github.PullRequestLatestOpinionatedReview{Author: github.ReviewAuthor{Login: "dismissed"}, State: github.ReviewStateDismissed},
	))

	repo := &Repository{gateway: gateway, log: silogtest.New(t)}
	var got []*forge.ReviewerState
	for state, err := range repo.ListReviewerStates(t.Context(), &PR{Number: 1, GQLID: "PR_1"}) {
		require.NoError(t, err)
		got = append(got, state)
	}
	require.Len(t, got, 2)
	assert.Equal(t, "approved", got[0].Reviewer)
	assert.Equal(t, forge.ReviewDispositionApprove, got[0].Disposition)
	assert.Equal(t, "1111111111111111111111111111111111111111", got[0].CommitHash.String())
	assert.Equal(t, submittedAt, got[0].SubmittedAt)
	assert.Equal(t, "changes", got[1].Reviewer)
	assert.Equal(t, forge.ReviewDispositionRequestChanges, got[1].Disposition)
}

func TestRepository_ListReviewThreads(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	line, originalLine, originalStartLine := 0, 19, 17
	createdAt := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	gateway.EXPECT().PullRequestReviewThreads(gomock.Any(), github.ID("PR_1"), nil).Return(threadSeq(
		&github.PullRequestReviewThread{
			ID: "T_file", Path: "file.go", SubjectType: github.ReviewThreadSubjectTypeFile,
		},
		&github.PullRequestReviewThread{
			ID: "T_1", Path: "a.go", SubjectType: github.ReviewThreadSubjectTypeLine,
			DiffSide: github.DiffSideLeft,
			Line:     &line, OriginalLine: &originalLine, OriginalStartLine: &originalStartLine,
			IsResolved: true, IsOutdated: true,
			Comments: []github.PullRequestReviewComment{{
				ID: "C_1", URL: "https://example.com/c1", Body: "first",
				Author: github.ReviewAuthor{Login: "octo"}, CreatedAt: createdAt,
			}},
		},
	))

	repo := &Repository{gateway: gateway, log: silogtest.New(t)}
	var got []*forge.ReviewThread
	for thread, err := range repo.ListReviewThreads(t.Context(), &PR{Number: 1, GQLID: "PR_1"}) {
		require.NoError(t, err)
		got = append(got, thread)
	}
	require.Len(t, got, 1)
	assert.Equal(t, forge.ReviewThreadRange{StartLine: 17, EndLine: 19}, got[0].Range)
	assert.Equal(t, forge.ReviewThreadSideLeft, got[0].Side)
	assert.Equal(t, true, *got[0].Resolved)
	assert.Equal(t, true, *got[0].Outdated)
	require.Len(t, got[0].Comments, 1)
	commentID, ok := got[0].Comments[0].ID.(*PRReviewComment)
	require.True(t, ok)
	assert.Equal(t, github.ID("C_1"), commentID.GQLID)
	assert.Equal(t, "octo", got[0].Comments[0].Author)
	assert.Equal(t, createdAt, got[0].Comments[0].CreatedAt)
}

func TestRepository_SubmitReview_preservesMixedCommentOrder(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	repo := &Repository{gateway: gateway, log: silogtest.New(t)}

	gomock.InOrder(
		gateway.EXPECT().AddPullRequestReview(gomock.Any(), &github.AddPullRequestReviewInput{PullRequestID: "PR_1"}).Return(&github.AddedPullRequestReview{ID: "R_1"}, nil),
		gateway.EXPECT().AddPullRequestReviewThread(gomock.Any(), &github.AddPullRequestReviewThreadInput{
			PullRequestReviewID: "R_1", Path: "a.go", Line: 9, Side: github.DiffSideLeft,
			StartLine: new(7), StartSide: new(github.DiffSideLeft), Body: "first",
		}).Return(&github.AddedPullRequestReviewThread{ID: "T_1", Comment: &github.AddedPullRequestReviewComment{ID: "C_1", URL: "https://example.com/c1"}}, nil),
		gateway.EXPECT().AddPullRequestReviewThreadReply(gomock.Any(), &github.AddPullRequestReviewThreadReplyInput{
			PullRequestReviewThreadID: "T_existing", PullRequestReviewID: "R_1", Body: "reply",
		}).Return(&github.AddedPullRequestReviewComment{ID: "C_2", URL: "https://example.com/c2"}, nil),
		gateway.EXPECT().AddPullRequestReviewThread(gomock.Any(), &github.AddPullRequestReviewThreadInput{
			PullRequestReviewID: "R_1", Path: "b.go", Line: 4, Side: github.DiffSideRight, Body: "last",
		}).Return(&github.AddedPullRequestReviewThread{ID: "T_2", Comment: &github.AddedPullRequestReviewComment{ID: "C_3", URL: "https://example.com/c3"}}, nil),
		gateway.EXPECT().SubmitPullRequestReview(gomock.Any(), &github.SubmitPullRequestReviewInput{
			PullRequestReviewID: "R_1", Event: github.ReviewEventRequestChanges, Body: "summary",
		}).Return(nil),
	)

	result, err := repo.SubmitReview(t.Context(), &PR{Number: 1, GQLID: "PR_1"}, forge.SubmitReviewRequest{
		Body: "summary", Disposition: forge.ReviewDispositionRequestChanges,
		Comments: []forge.SubmitReviewCommentRequest{
			{Path: "a.go", Range: forge.ReviewThreadRange{StartLine: 7, EndLine: 9}, Side: forge.ReviewThreadSideLeft, Body: "first"},
			{
				ReplyTo: &PRReviewThread{GQLID: "T_existing"},
				Path:    "", Range: forge.ReviewThreadRange{StartLine: 9, EndLine: 1},
				Side: forge.ReviewThreadSide(42), Body: "reply",
			},
			{Path: "b.go", Range: forge.ReviewThreadLine(4), Side: forge.ReviewThreadSideRight, Body: "last"},
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Comments, 3)
	assert.Equal(t, github.ID("C_1"), result.Comments[0].CommentID.(*PRReviewComment).GQLID)
	assert.Equal(t, github.ID("T_existing"), result.Comments[1].ThreadID.(*PRReviewThread).GQLID)
	assert.Equal(t, github.ID("C_3"), result.Comments[2].CommentID.(*PRReviewComment).GQLID)
}

func TestRepository_SubmitReview_withoutDispositionPublishesContentDirectly(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	repo := &Repository{gateway: gateway, log: silogtest.New(t)}

	gomock.InOrder(
		gateway.EXPECT().AddComment(gomock.Any(), github.ID("PR_1"), "summary").Return(&github.AddedComment{}, nil),
		gateway.EXPECT().AddPullRequestReviewThread(gomock.Any(), &github.AddPullRequestReviewThreadInput{
			PullRequestID: "PR_1", Path: "a.go", Line: 9, Side: github.DiffSideLeft, Body: "first",
		}).Return(&github.AddedPullRequestReviewThread{ID: "T_1", Comment: &github.AddedPullRequestReviewComment{ID: "C_1", URL: "https://example.com/c1"}}, nil),
		gateway.EXPECT().AddPullRequestReviewThreadReply(gomock.Any(), &github.AddPullRequestReviewThreadReplyInput{
			PullRequestReviewThreadID: "T_existing", Body: "reply",
		}).Return(&github.AddedPullRequestReviewComment{ID: "C_2", URL: "https://example.com/c2"}, nil),
	)

	result, err := repo.SubmitReview(t.Context(), &PR{Number: 1, GQLID: "PR_1"}, forge.SubmitReviewRequest{
		Body: "summary",
		Comments: []forge.SubmitReviewCommentRequest{
			{Path: "a.go", Range: forge.ReviewThreadLine(9), Side: forge.ReviewThreadSideLeft, Body: "first"},
			{ReplyTo: &PRReviewThread{GQLID: "T_existing"}, Body: "reply"},
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Comments, 2)
	assert.Equal(t, github.ID("T_1"), result.Comments[0].ThreadID.(*PRReviewThread).GQLID)
	assert.Equal(t, github.ID("C_1"), result.Comments[0].CommentID.(*PRReviewComment).GQLID)
	assert.Equal(t, github.ID("T_existing"), result.Comments[1].ThreadID.(*PRReviewThread).GQLID)
	assert.Equal(t, github.ID("C_2"), result.Comments[1].CommentID.(*PRReviewComment).GQLID)
}

func TestRepository_SubmitReview_withoutDispositionReportsPullRequestCommentError(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	repo := &Repository{gateway: gateway, log: silogtest.New(t)}

	gateway.EXPECT().
		AddComment(gomock.Any(), github.ID("PR_1"), "summary").
		Return(nil, errors.New("boom"))

	_, err := repo.SubmitReview(t.Context(), &PR{Number: 1, GQLID: "PR_1"}, forge.SubmitReviewRequest{
		Body: "summary",
	})
	assert.EqualError(t, err, "post pull request comment: boom")
}

func TestRepository_SubmitReview_panicsForUnknownSide(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	repo := &Repository{gateway: gateway, log: silogtest.New(t)}

	assert.PanicsWithValue(
		t,
		"unexpected review thread side: ReviewThreadSide(42)",
		func() {
			_, _ = repo.SubmitReview(
				t.Context(),
				&PR{Number: 1, GQLID: "PR_1"},
				forge.SubmitReviewRequest{Comments: []forge.SubmitReviewCommentRequest{
					{
						Path: "a.go", Range: forge.ReviewThreadLine(1),
						Side: forge.ReviewThreadSide(42), Body: "comment",
					},
				}},
			)
		},
	)
}

func TestRepository_ReviewCommentAndResolutionMutations(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	repo := &Repository{gateway: gateway, log: silogtest.New(t)}
	comment := &PRReviewComment{GQLID: "C_1", URL: "https://example.com/c1"}
	thread := &PRReviewThread{GQLID: "T_1"}

	gateway.EXPECT().UpdatePullRequestReviewComment(gomock.Any(), github.ID("C_1"), "updated").Return(nil)
	require.NoError(t, repo.UpdateReviewComment(t.Context(), comment, "updated"))
	gateway.EXPECT().ResolveReviewThread(gomock.Any(), github.ID("T_1")).Return(nil)
	require.NoError(t, repo.ResolveReviewThread(t.Context(), thread))
	gateway.EXPECT().UnresolveReviewThread(gomock.Any(), github.ID("T_1")).Return(nil)
	require.NoError(t, repo.UnresolveReviewThread(t.Context(), thread))
}

func TestRepository_IDsRejectWrongDomain(t *testing.T) {
	gateway := NewMockGithubGateway(gomock.NewController(t))
	repo := &Repository{gateway: gateway, log: silogtest.New(t)}

	assert.PanicsWithValue(
		t,
		"unexpected PR review thread type: github.foreignReviewThreadID",
		func() {
			_ = repo.ResolveReviewThread(t.Context(), foreignReviewThreadID("T_1"))
		},
	)
	assert.PanicsWithValue(
		t,
		"unexpected PR review comment type: *github.PRComment",
		func() {
			_ = repo.UpdateReviewComment(t.Context(), &PRComment{GQLID: "C_1"}, "body")
		},
	)
	assert.PanicsWithValue(
		t,
		"unexpected PR comment type: *github.PRReviewComment",
		func() {
			_ = repo.UpdateChangeComment(t.Context(), &PRReviewComment{GQLID: "C_1"}, "body")
		},
	)
}

func reviewSeq(reviews ...*github.PullRequestLatestOpinionatedReview) iter.Seq2[*github.PullRequestLatestOpinionatedReview, error] {
	return func(yield func(*github.PullRequestLatestOpinionatedReview, error) bool) {
		for _, review := range reviews {
			if !yield(review, nil) {
				return
			}
		}
	}
}

func threadSeq(threads ...*github.PullRequestReviewThread) iter.Seq2[*github.PullRequestReviewThread, error] {
	return func(yield func(*github.PullRequestReviewThread, error) bool) {
		for _, thread := range threads {
			if !yield(thread, nil) {
				return
			}
		}
	}
}

// foreignReviewThreadID is a non-GitHub thread ID for provider-domain tests.
type foreignReviewThreadID string

func (id foreignReviewThreadID) String() string { return string(id) }
