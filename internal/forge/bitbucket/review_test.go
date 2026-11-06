package bitbucket

import (
	"context"
	"errors"
	"iter"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	gw "go.abhg.dev/gs/internal/gateway/bitbucket"
	"go.abhg.dev/gs/internal/silog"
	"go.uber.org/mock/gomock"
)

func TestRepository_reviewCapability(t *testing.T) {
	ctrl := gomock.NewController(t)
	base := newRepository(new(Forge), silog.Nop(), NewMockGateway(ctrl))
	assert.NotImplements(t, (*forge.ReviewRepository)(nil), base)

	reviewGW := &stubReviewGateway{
		Gateway: NewMockGateway(ctrl),
		capabilities: gw.ReviewCapabilities{
			Supported: true,
		},
	}
	repo := mustWithReviewRepository(t.Context(), t, newRepository(new(Forge), silog.Nop(), reviewGW))
	assert.Implements(t, (*forge.ReviewRepository)(nil), repo)
	assert.Implements(t, (*forge.ReviewCommentEditor)(nil), repo)
	assert.NotImplements(t, (*forge.ReviewThreadResolver)(nil), repo)

	reviewGW.capabilities.ThreadResolution = true
	repo = mustWithReviewRepository(t.Context(), t, newRepository(new(Forge), silog.Nop(), reviewGW))
	assert.Implements(t, (*forge.ReviewThreadResolver)(nil), repo)
}

func TestReviewRepository_ListReviewThreads(t *testing.T) {
	resolved := true
	created := time.Date(2026, time.June, 14, 12, 0, 0, 0, time.UTC)
	reviewGW := &stubReviewGateway{
		Gateway: NewMockGateway(gomock.NewController(t)),
		listReviewThreads: func(context.Context, int64) iter.Seq2[*gw.ReviewThread, error] {
			return func(yield func(*gw.ReviewThread, error) bool) {
				yield(&gw.ReviewThread{
					RootCommentID: 10,
					CommitHash:    "1111111111111111111111111111111111111111",
					Path:          "review.go",
					Range:         forge.ReviewThreadLine(3),
					Side:          forge.ReviewThreadSideRight,
					Resolved:      resolved,
					Comments: []gw.ReviewComment{
						{ID: 10, Body: "root", Author: "spock", CreatedAt: created},
						{ID: 11, Body: "reply", Author: "kirk", CreatedAt: created.Add(time.Minute)},
					},
				}, nil)
			}
		},
	}
	repo := mustWithReviewRepository(t.Context(), t, newRepository(new(Forge), silog.Nop(), reviewGW)).(forge.ReviewRepository)

	var got []*forge.ReviewThread
	for thread, err := range repo.ListReviewThreads(t.Context(), &PR{Number: 7}) {
		require.NoError(t, err)
		got = append(got, thread)
	}

	require.Len(t, got, 1)
	assert.Equal(t, "10:7", got[0].ID.String())
	assert.Equal(t, "1111111111111111111111111111111111111111", got[0].CommitHash.String())
	assert.Equal(t, reviewCommentID{CommentID: 10, PRID: 7}, got[0].Comments[0].ID)
	assert.Equal(t, reviewCommentID{CommentID: 11, PRID: 7}, got[0].Comments[1].ID)
	assert.Equal(t, &resolved, got[0].Resolved)
	assert.Nil(t, got[0].Outdated)
}

func TestReviewRepository_ListReviewerStates(t *testing.T) {
	submitted := time.Date(2026, time.June, 14, 12, 0, 0, 0, time.UTC)
	reviewGW := &stubReviewGateway{
		Gateway: NewMockGateway(gomock.NewController(t)),
		listReviewerStates: func(context.Context, int64) iter.Seq2[*gw.ReviewerState, error] {
			return func(yield func(*gw.ReviewerState, error) bool) {
				yield(&gw.ReviewerState{
					Reviewer:    "spock",
					Disposition: forge.ReviewDispositionApprove,
					SubmittedAt: submitted,
				}, nil)
			}
		},
	}
	repo := mustWithReviewRepository(t.Context(), t, newRepository(new(Forge), silog.Nop(), reviewGW)).(forge.ReviewRepository)

	var got []*forge.ReviewerState
	for state, err := range repo.ListReviewerStates(t.Context(), &PR{Number: 7}) {
		require.NoError(t, err)
		got = append(got, state)
	}

	assert.Equal(t, []*forge.ReviewerState{{
		Reviewer:    "spock",
		Disposition: forge.ReviewDispositionApprove,
		SubmittedAt: submitted,
	}}, got)
}

func TestReviewRepository_SubmitReview_degradesUnsupportedMultiline(t *testing.T) {
	var actions []string
	base := &stubReviewGateway{
		Gateway: NewMockGateway(gomock.NewController(t)),
		capabilities: gw.ReviewCapabilities{
			Supported:    true,
			NativeDrafts: true,
		},
		createReviewComment: func(
			_ context.Context,
			_ int64,
			req gw.CreateReviewCommentRequest,
		) (*gw.ReviewComment, error) {
			actions = append(actions, "draft")
			assert.Equal(t, forge.ReviewThreadLine(3), req.Range)
			return &gw.ReviewComment{ID: 10}, nil
		},
	}
	reviewGW := &stubPendingReviewGateway{
		stubReviewGateway: base,
		reviewContext: func(context.Context, int64) (gw.ReviewContext, error) {
			actions = append(actions, "context")
			return gw.ReviewContext{}, nil
		},
		reviewAnchor: func(
			_ context.Context,
			_ int64,
			_ gw.ReviewContext,
			_ string,
			lines forge.ReviewThreadRange,
			_ forge.ReviewThreadSide,
		) (gw.ReviewAnchor, error) {
			actions = append(actions, "anchor")
			assert.Equal(t, forge.ReviewThreadLine(3), lines)
			return gw.ReviewAnchor{EndLineType: "ADDED"}, nil
		},
		publishReview: func(
			context.Context,
			int64,
			gw.ReviewContext,
			string,
			forge.ReviewDisposition,
		) error {
			actions = append(actions, "publish")
			return nil
		},
	}
	repo := mustWithReviewRepository(
		t.Context(), t, newRepository(new(Forge), silog.Nop(), reviewGW),
	).(forge.ReviewRepository)

	result, err := repo.SubmitReview(
		t.Context(), &PR{Number: 7}, forge.SubmitReviewRequest{
			Comments: []forge.SubmitReviewCommentRequest{{
				Path:  "review.go",
				Range: forge.ReviewThreadRange{StartLine: 3, EndLine: 4},
				Body:  "comment",
			}},
		})

	require.NoError(t, err)
	require.Len(t, result.Comments, 1)
	assert.Equal(t, []string{"context", "anchor", "draft", "publish"}, actions)
}

func TestReviewRepository_SubmitReview_fileLevel(t *testing.T) {
	reviewGW := &stubReviewGateway{
		Gateway: NewMockGateway(gomock.NewController(t)),
		capabilities: gw.ReviewCapabilities{
			Supported: true,
			FileLevel: true,
		},
		createReviewComment: func(
			_ context.Context,
			_ int64,
			req gw.CreateReviewCommentRequest,
		) (*gw.ReviewComment, error) {
			assert.True(t, req.Range.IsZero())
			assert.Equal(t, forge.ReviewThreadSide(99), req.Side)
			return &gw.ReviewComment{ID: 10}, nil
		},
	}
	repo := mustWithReviewRepository(
		t.Context(), t, newRepository(new(Forge), silog.Nop(), reviewGW),
	).(forge.ReviewRepository)

	result, err := repo.SubmitReview(
		t.Context(), &PR{Number: 7}, forge.SubmitReviewRequest{
			Comments: []forge.SubmitReviewCommentRequest{{
				Path: "review.go",
				Side: forge.ReviewThreadSide(99),
				Body: "whole file",
			}},
		})
	require.NoError(t, err)
	require.Len(t, result.Comments, 1)
	assert.Equal(t, "10:7", result.Comments[0].ThreadID.String())
}

func TestReviewRepository_SubmitReview_nativeFileLevel(t *testing.T) {
	var actions []string
	base := &stubReviewGateway{
		Gateway: NewMockGateway(gomock.NewController(t)),
		capabilities: gw.ReviewCapabilities{
			Supported:    true,
			NativeDrafts: true,
			FileLevel:    true,
		},
		createReviewComment: func(
			_ context.Context,
			_ int64,
			req gw.CreateReviewCommentRequest,
		) (*gw.ReviewComment, error) {
			actions = append(actions, "draft")
			assert.True(t, req.Range.IsZero())
			assert.Zero(t, req.ReviewAnchor)
			return &gw.ReviewComment{ID: 10}, nil
		},
	}
	reviewGW := &stubPendingReviewGateway{
		stubReviewGateway: base,
		reviewContext: func(context.Context, int64) (gw.ReviewContext, error) {
			actions = append(actions, "context")
			return gw.ReviewContext{}, nil
		},
		reviewAnchor: func(
			_ context.Context,
			_ int64,
			_ gw.ReviewContext,
			_ string,
			lines forge.ReviewThreadRange,
			side forge.ReviewThreadSide,
		) (gw.ReviewAnchor, error) {
			actions = append(actions, "anchor")
			assert.True(t, lines.IsZero())
			assert.Equal(t, forge.ReviewThreadSide(99), side)
			return gw.ReviewAnchor{}, nil
		},
		publishReview: func(
			context.Context,
			int64,
			gw.ReviewContext,
			string,
			forge.ReviewDisposition,
		) error {
			actions = append(actions, "publish")
			return nil
		},
	}
	repo := mustWithReviewRepository(
		t.Context(), t, newRepository(new(Forge), silog.Nop(), reviewGW),
	).(forge.ReviewRepository)

	_, err := repo.SubmitReview(
		t.Context(), &PR{Number: 7}, forge.SubmitReviewRequest{
			Comments: []forge.SubmitReviewCommentRequest{{
				Path: "review.go",
				Side: forge.ReviewThreadSide(99),
				Body: "whole file",
			}},
		})
	require.NoError(t, err)
	assert.Equal(t, []string{"context", "anchor", "draft", "publish"}, actions)
}

func TestReviewRepository_SubmitReview_invalidDispositionPanicsBeforeMutation(t *testing.T) {
	var mutations int
	mockGateway := NewMockGateway(gomock.NewController(t))
	mockGateway.EXPECT().
		CreateComment(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, int64, string) (*gw.ChangeComment, error) {
			mutations++
			return &gw.ChangeComment{}, nil
		}).
		AnyTimes()
	reviewGW := &stubReviewGateway{
		Gateway: mockGateway,
		createReviewComment: func(
			context.Context,
			int64,
			gw.CreateReviewCommentRequest,
		) (*gw.ReviewComment, error) {
			mutations++
			return &gw.ReviewComment{ID: 10}, nil
		},
		setReviewDisposition: func(
			context.Context,
			int64,
			forge.ReviewDisposition,
		) error {
			mutations++
			return nil
		},
	}
	repo := mustWithReviewRepository(
		t.Context(), t, newRepository(new(Forge), silog.Nop(), reviewGW),
	).(forge.ReviewRepository)

	assert.Panics(t, func() {
		_, _ = repo.SubmitReview(
			t.Context(), &PR{Number: 7}, forge.SubmitReviewRequest{
				Body:        "summary",
				Disposition: forge.ReviewDisposition(99),
				Comments: []forge.SubmitReviewCommentRequest{{
					Path:  "review.go",
					Range: forge.ReviewThreadLine(3),
					Body:  "comment",
				}},
			})
	})
	assert.Zero(t, mutations)

	nativeMutations := 0
	nativeBase := &stubReviewGateway{
		Gateway: NewMockGateway(gomock.NewController(t)),
		capabilities: gw.ReviewCapabilities{
			Supported:    true,
			NativeDrafts: true,
			Multiline:    true,
		},
		createReviewComment: func(
			context.Context,
			int64,
			gw.CreateReviewCommentRequest,
		) (*gw.ReviewComment, error) {
			nativeMutations++
			return &gw.ReviewComment{ID: 10}, nil
		},
		setReviewDisposition: func(
			context.Context,
			int64,
			forge.ReviewDisposition,
		) error {
			nativeMutations++
			return nil
		},
	}
	nativeGW := &stubPendingReviewGateway{
		stubReviewGateway: nativeBase,
		reviewContext: func(context.Context, int64) (gw.ReviewContext, error) {
			nativeMutations++
			return gw.ReviewContext{}, nil
		},
		reviewAnchor: func(
			context.Context,
			int64,
			gw.ReviewContext,
			string,
			forge.ReviewThreadRange,
			forge.ReviewThreadSide,
		) (gw.ReviewAnchor, error) {
			nativeMutations++
			return gw.ReviewAnchor{}, nil
		},
		publishReview: func(
			context.Context,
			int64,
			gw.ReviewContext,
			string,
			forge.ReviewDisposition,
		) error {
			nativeMutations++
			return nil
		},
	}
	nativeRepo := mustWithReviewRepository(
		t.Context(), t, newRepository(new(Forge), silog.Nop(), nativeGW),
	).(forge.ReviewRepository)

	assert.Panics(t, func() {
		_, _ = nativeRepo.SubmitReview(
			t.Context(), &PR{Number: 7}, forge.SubmitReviewRequest{
				Body:        "summary",
				Disposition: forge.ReviewDisposition(99),
				Comments: []forge.SubmitReviewCommentRequest{{
					Path:  "review.go",
					Range: forge.ReviewThreadLine(3),
					Body:  "comment",
				}},
			})
	})
	assert.Zero(t, nativeMutations)
}

func TestReviewRepository_SubmitReview_commentErrorReturnsEmptyResult(t *testing.T) {
	wantErr := errors.New("second comment failed")
	var calls int
	reviewGW := &stubReviewGateway{
		Gateway: NewMockGateway(gomock.NewController(t)),
		createReviewComment: func(
			_ context.Context,
			_ int64,
			req gw.CreateReviewCommentRequest,
		) (*gw.ReviewComment, error) {
			calls++
			if calls == 2 {
				return nil, wantErr
			}
			assert.Equal(t, forge.ReviewThreadSideLeft, req.Side)
			return &gw.ReviewComment{ID: 10, Body: req.Body}, nil
		},
	}
	repo := mustWithReviewRepository(t.Context(), t, newRepository(new(Forge), silog.Nop(), reviewGW)).(forge.ReviewRepository)

	result, err := repo.SubmitReview(t.Context(), &PR{Number: 7}, forge.SubmitReviewRequest{
		Comments: []forge.SubmitReviewCommentRequest{
			{Path: "review.go", Range: forge.ReviewThreadLine(3), Side: forge.ReviewThreadSideLeft, Body: "first"},
			{Path: "review.go", Range: forge.ReviewThreadLine(4), Body: "second"},
		},
	})
	require.ErrorIs(t, err, wantErr)
	assert.Empty(t, result.Comments)
}

func TestReviewRepository_SubmitReview_dispositionErrorReturnsEmptyResult(t *testing.T) {
	wantErr := errors.New("request changes failed")
	reviewGW := &stubReviewGateway{
		Gateway: NewMockGateway(gomock.NewController(t)),
		createReviewComment: func(
			_ context.Context,
			_ int64,
			req gw.CreateReviewCommentRequest,
		) (*gw.ReviewComment, error) {
			return &gw.ReviewComment{ID: 10, Body: req.Body}, nil
		},
		setReviewDisposition: func(
			context.Context,
			int64,
			forge.ReviewDisposition,
		) error {
			return wantErr
		},
	}
	repo := mustWithReviewRepository(
		t.Context(), t, newRepository(new(Forge), silog.Nop(), reviewGW),
	).(forge.ReviewRepository)

	result, err := repo.SubmitReview(
		t.Context(), &PR{Number: 7}, forge.SubmitReviewRequest{
			Disposition: forge.ReviewDispositionRequestChanges,
			Comments: []forge.SubmitReviewCommentRequest{{
				Path:  "review.go",
				Range: forge.ReviewThreadLine(3),
				Body:  "comment",
			}},
		})
	require.ErrorIs(t, err, wantErr)
	assert.Empty(t, result.Comments)
}

func TestReviewRepository_SubmitReview_reply(t *testing.T) {
	reviewGW := &stubReviewGateway{
		Gateway: NewMockGateway(gomock.NewController(t)),
		createReviewComment: func(
			_ context.Context,
			prID int64,
			req gw.CreateReviewCommentRequest,
		) (*gw.ReviewComment, error) {
			assert.Equal(t, int64(7), prID)
			assert.Equal(t, int64(10), req.ParentID)
			return &gw.ReviewComment{ID: 11, Body: req.Body}, nil
		},
	}
	repo := mustWithReviewRepository(t.Context(), t, newRepository(new(Forge), silog.Nop(), reviewGW)).(forge.ReviewRepository)
	threadID := reviewThreadID{CommentID: 10, PRID: 7}

	result, err := repo.SubmitReview(t.Context(), &PR{Number: 7}, forge.SubmitReviewRequest{
		Comments: []forge.SubmitReviewCommentRequest{{
			ReplyTo: threadID,
			Body:    "reply",
		}},
	})
	require.NoError(t, err)
	require.Len(t, result.Comments, 1)
	assert.Equal(t, threadID, result.Comments[0].ThreadID)
	assert.Equal(t, reviewCommentID{CommentID: 11, PRID: 7}, result.Comments[0].CommentID)
}

func TestReviewRepository_SubmitReview_bodyAndDisposition(t *testing.T) {
	mockGateway := NewMockGateway(gomock.NewController(t))
	mockGateway.EXPECT().CreateComment(gomock.Any(), int64(7), "summary").
		Return(&gw.ChangeComment{ID: 9, PRID: 7}, nil)
	var gotDisposition forge.ReviewDisposition
	reviewGW := &stubReviewGateway{
		Gateway: mockGateway,
		setReviewDisposition: func(
			_ context.Context,
			_ int64,
			disposition forge.ReviewDisposition,
		) error {
			gotDisposition = disposition
			return nil
		},
	}
	repo := mustWithReviewRepository(t.Context(), t, newRepository(new(Forge), silog.Nop(), reviewGW)).(forge.ReviewRepository)

	result, err := repo.SubmitReview(t.Context(), &PR{Number: 7}, forge.SubmitReviewRequest{
		Body:        "summary",
		Disposition: forge.ReviewDispositionRequestChanges,
	})
	require.NoError(t, err)
	assert.Empty(t, result.Comments)
	assert.Equal(t, forge.ReviewDispositionRequestChanges, gotDisposition)
}

func TestReviewRepository_SubmitReview_noneDoesNotSetDisposition(t *testing.T) {
	mockGateway := NewMockGateway(gomock.NewController(t))
	mockGateway.EXPECT().CreateComment(gomock.Any(), int64(7), "summary").
		Return(&gw.ChangeComment{ID: 9, PRID: 7}, nil)
	reviewGW := &stubReviewGateway{
		Gateway: mockGateway,
		createReviewComment: func(
			_ context.Context,
			_ int64,
			req gw.CreateReviewCommentRequest,
		) (*gw.ReviewComment, error) {
			return &gw.ReviewComment{ID: 10, Body: req.Body}, nil
		},
		setReviewDisposition: func(
			context.Context,
			int64,
			forge.ReviewDisposition,
		) error {
			t.Fatal("content-only submission must not set a disposition")
			return nil
		},
	}
	repo := mustWithReviewRepository(
		t.Context(), t, newRepository(new(Forge), silog.Nop(), reviewGW),
	).(forge.ReviewRepository)

	result, err := repo.SubmitReview(
		t.Context(), &PR{Number: 7}, forge.SubmitReviewRequest{
			Body: "summary",
			Comments: []forge.SubmitReviewCommentRequest{{
				Path:  "review.go",
				Range: forge.ReviewThreadLine(3),
				Body:  "comment",
			}},
		})
	require.NoError(t, err)
	require.Len(t, result.Comments, 1)
	assert.Equal(t, reviewCommentID{CommentID: 10, PRID: 7}, result.Comments[0].CommentID)
}

func TestReviewRepository_SubmitReview_nativeDrafts(t *testing.T) {
	var actions []string
	base := &stubReviewGateway{
		Gateway: NewMockGateway(gomock.NewController(t)),
		capabilities: gw.ReviewCapabilities{
			Supported:    true,
			NativeDrafts: true,
			Multiline:    true,
		},
		createReviewComment: func(
			_ context.Context,
			_ int64,
			req gw.CreateReviewCommentRequest,
		) (*gw.ReviewComment, error) {
			actions = append(actions, "draft")
			assert.Equal(t, gw.ReviewContext{
				BaseHash: "base-sha",
				HeadHash: "head-sha",
				Version:  4,
			}, req.ReviewContext)
			assert.Equal(t, gw.ReviewAnchor{
				StartLineType: "ADDED",
				EndLineType:   "ADDED",
			}, req.ReviewAnchor)
			assert.Equal(t,
				forge.ReviewThreadRange{StartLine: 3, EndLine: 4},
				req.Range)
			return &gw.ReviewComment{ID: 10}, nil
		},
		setReviewDisposition: func(
			context.Context,
			int64,
			forge.ReviewDisposition,
		) error {
			t.Fatal("native review must not update the participant separately")
			return nil
		},
	}
	reviewGW := &stubPendingReviewGateway{
		stubReviewGateway: base,
		reviewContext: func(context.Context, int64) (gw.ReviewContext, error) {
			actions = append(actions, "context")
			return gw.ReviewContext{
				BaseHash: "base-sha",
				HeadHash: "head-sha",
				Version:  4,
			}, nil
		},
		reviewAnchor: func(
			_ context.Context,
			_ int64,
			_ gw.ReviewContext,
			_ string,
			lines forge.ReviewThreadRange,
			_ forge.ReviewThreadSide,
		) (gw.ReviewAnchor, error) {
			actions = append(actions, "anchor")
			assert.Equal(t,
				forge.ReviewThreadRange{StartLine: 3, EndLine: 4},
				lines)
			return gw.ReviewAnchor{
				StartLineType: "ADDED",
				EndLineType:   "ADDED",
			}, nil
		},
		publishReview: func(
			_ context.Context,
			_ int64,
			_ gw.ReviewContext,
			body string,
			disposition forge.ReviewDisposition,
		) error {
			actions = append(actions, "publish")
			assert.Equal(t, "summary", body)
			assert.Equal(t, forge.ReviewDispositionRequestChanges, disposition)
			return nil
		},
	}
	repo := mustWithReviewRepository(
		t.Context(), t, newRepository(new(Forge), silog.Nop(), reviewGW),
	).(forge.ReviewRepository)

	result, err := repo.SubmitReview(
		t.Context(), &PR{Number: 7}, forge.SubmitReviewRequest{
			Body:        "summary",
			Disposition: forge.ReviewDispositionRequestChanges,
			Comments: []forge.SubmitReviewCommentRequest{{
				Path:  "review.go",
				Range: forge.ReviewThreadRange{StartLine: 3, EndLine: 4},
				Body:  "comment",
			}},
		})
	require.NoError(t, err)
	assert.Equal(t, []string{"context", "anchor", "draft", "publish"}, actions)
	require.Len(t, result.Comments, 1)
	assert.Equal(t, "10:7", result.Comments[0].ThreadID.String())
}

func TestReviewRepository_SubmitReview_nativeDispositionOnly(t *testing.T) {
	var actions []string
	base := &stubReviewGateway{
		Gateway: NewMockGateway(gomock.NewController(t)),
		capabilities: gw.ReviewCapabilities{
			Supported:    true,
			NativeDrafts: true,
			Multiline:    true,
		},
		setReviewDisposition: func(
			context.Context,
			int64,
			forge.ReviewDisposition,
		) error {
			t.Fatal("native review must not update the participant separately")
			return nil
		},
	}
	reviewGW := &stubPendingReviewGateway{
		stubReviewGateway: base,
		reviewContext: func(context.Context, int64) (gw.ReviewContext, error) {
			actions = append(actions, "context")
			return gw.ReviewContext{HeadHash: "head-sha", Version: 4}, nil
		},
		publishReview: func(
			_ context.Context,
			_ int64,
			review gw.ReviewContext,
			body string,
			disposition forge.ReviewDisposition,
		) error {
			actions = append(actions, "publish")
			assert.Equal(t, gw.ReviewContext{
				HeadHash: "head-sha",
				Version:  4,
			}, review)
			assert.Empty(t, body)
			assert.Equal(t, forge.ReviewDispositionApprove, disposition)
			return nil
		},
	}
	repo := mustWithReviewRepository(
		t.Context(), t, newRepository(new(Forge), silog.Nop(), reviewGW),
	).(forge.ReviewRepository)

	result, err := repo.SubmitReview(
		t.Context(), &PR{Number: 7}, forge.SubmitReviewRequest{
			Disposition: forge.ReviewDispositionApprove,
		})
	require.NoError(t, err)
	assert.Empty(t, result.Comments)
	assert.Equal(t, []string{"context", "publish"}, actions)
}

func TestReviewRepository_UpdateAndResolve(t *testing.T) {
	mockGateway := NewMockGateway(gomock.NewController(t))
	mockGateway.EXPECT().UpdateComment(gomock.Any(), &gw.ChangeComment{
		ID:   11,
		PRID: 7,
	}, "edited").Return(nil)
	var actions []string
	reviewGW := &stubReviewGateway{
		Gateway: mockGateway,
		resolveReviewThread: func(_ context.Context, prID, commentID int64) error {
			actions = append(actions, "resolve")
			assert.Equal(t, int64(7), prID)
			assert.Equal(t, int64(10), commentID)
			return nil
		},
		unresolveReviewThread: func(_ context.Context, prID, commentID int64) error {
			actions = append(actions, "unresolve")
			assert.Equal(t, int64(7), prID)
			assert.Equal(t, int64(10), commentID)
			return nil
		},
	}
	repo := mustWithReviewRepository(t.Context(), t, newRepository(new(Forge), silog.Nop(), reviewGW))

	require.NoError(t, repo.(forge.ReviewCommentEditor).UpdateReviewComment(
		t.Context(), reviewCommentID{CommentID: 11, PRID: 7}, "edited"))
	threadID := reviewThreadID{CommentID: 10, PRID: 7}
	resolver := repo.(forge.ReviewThreadResolver)
	require.NoError(t, resolver.ResolveReviewThread(t.Context(), threadID))
	require.NoError(t, resolver.UnresolveReviewThread(t.Context(), threadID))
	assert.Equal(t, []string{"resolve", "unresolve"}, actions)
}

func TestReviewRepository_internalIDsPanic(t *testing.T) {
	reviewGW := &stubReviewGateway{
		Gateway: NewMockGateway(gomock.NewController(t)),
	}
	repo := mustWithReviewRepository(t.Context(), t, newRepository(new(Forge), silog.Nop(), reviewGW))

	assert.Panics(t, func() {
		_ = repo.(forge.ReviewCommentEditor).UpdateReviewComment(
			t.Context(), &PRComment{ID: 11, PRID: 7}, "edited")
	})
	assert.Panics(t, func() {
		_, _ = repo.(forge.ReviewRepository).SubmitReview(
			t.Context(), &PR{Number: 7}, forge.SubmitReviewRequest{
				Comments: []forge.SubmitReviewCommentRequest{{
					ReplyTo: reviewThreadID{CommentID: 10, PRID: 8},
					Body:    "reply",
				}},
			})
	})
}

type stubReviewGateway struct {
	gw.Gateway
	capabilities          gw.ReviewCapabilities
	listReviewerStates    func(context.Context, int64) iter.Seq2[*gw.ReviewerState, error]
	listReviewThreads     func(context.Context, int64) iter.Seq2[*gw.ReviewThread, error]
	createReviewComment   func(context.Context, int64, gw.CreateReviewCommentRequest) (*gw.ReviewComment, error)
	setReviewDisposition  func(context.Context, int64, forge.ReviewDisposition) error
	resolveReviewThread   func(context.Context, int64, int64) error
	unresolveReviewThread func(context.Context, int64, int64) error
}

func mustWithReviewRepository(
	ctx context.Context,
	t *testing.T,
	repo *Repository,
) forge.Repository {
	t.Helper()
	wrapped, err := withReviewRepository(ctx, repo)
	require.NoError(t, err)
	return wrapped
}

type stubPendingReviewGateway struct {
	*stubReviewGateway
	reviewContext func(context.Context, int64) (gw.ReviewContext, error)
	reviewAnchor  func(context.Context, int64, gw.ReviewContext, string, forge.ReviewThreadRange, forge.ReviewThreadSide) (gw.ReviewAnchor, error)
	publishReview func(context.Context, int64, gw.ReviewContext, string, forge.ReviewDisposition) error
}

func (g *stubPendingReviewGateway) ReviewContext(
	ctx context.Context,
	prID int64,
) (gw.ReviewContext, error) {
	return g.reviewContext(ctx, prID)
}

func (g *stubPendingReviewGateway) ReviewAnchor(
	ctx context.Context,
	prID int64,
	review gw.ReviewContext,
	path string,
	lines forge.ReviewThreadRange,
	side forge.ReviewThreadSide,
) (gw.ReviewAnchor, error) {
	return g.reviewAnchor(ctx, prID, review, path, lines, side)
}

func (g *stubPendingReviewGateway) PublishReview(
	ctx context.Context,
	prID int64,
	review gw.ReviewContext,
	body string,
	disposition forge.ReviewDisposition,
) error {
	return g.publishReview(ctx, prID, review, body, disposition)
}

func (g *stubReviewGateway) ReviewCapabilities(
	context.Context,
) (gw.ReviewCapabilities, error) {
	if g.capabilities == (gw.ReviewCapabilities{}) {
		return gw.ReviewCapabilities{
			Supported:        true,
			FileLevel:        true,
			Multiline:        true,
			ThreadResolution: true,
		}, nil
	}
	return g.capabilities, nil
}

func (g *stubReviewGateway) ListReviewerStates(
	ctx context.Context,
	prID int64,
) iter.Seq2[*gw.ReviewerState, error] {
	return g.listReviewerStates(ctx, prID)
}

func (g *stubReviewGateway) ListReviewThreads(
	ctx context.Context,
	prID int64,
) iter.Seq2[*gw.ReviewThread, error] {
	return g.listReviewThreads(ctx, prID)
}

func (g *stubReviewGateway) CreateReviewComment(
	ctx context.Context,
	prID int64,
	req gw.CreateReviewCommentRequest,
) (*gw.ReviewComment, error) {
	return g.createReviewComment(ctx, prID, req)
}

func (g *stubReviewGateway) SetReviewDisposition(
	ctx context.Context,
	prID int64,
	disposition forge.ReviewDisposition,
) error {
	return g.setReviewDisposition(ctx, prID, disposition)
}

func (g *stubReviewGateway) ResolveReviewThread(
	ctx context.Context,
	prID int64,
	commentID int64,
) error {
	return g.resolveReviewThread(ctx, prID, commentID)
}

func (g *stubReviewGateway) UnresolveReviewThread(
	ctx context.Context,
	prID int64,
	commentID int64,
) error {
	return g.unresolveReviewThread(ctx, prID, commentID)
}
