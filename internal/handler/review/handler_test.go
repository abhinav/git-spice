package review

import (
	"context"
	"io"
	"iter"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	reviewmodel "go.abhg.dev/gs/internal/review"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.uber.org/mock/gomock"
)

func TestDraftHandler_SaveCommentDraft(t *testing.T) {
	ctrl := gomock.NewController(t)
	worktree := NewMockWorktree(ctrl)
	store := NewMockStore(ctrl)
	handler := &DraftHandler{
		Log:      silog.Nop(),
		Worktree: worktree,
		Store:    store,
		Editor: func(context.Context, string) (string, error) {
			t.Fatal("editor should not open when a message is supplied")
			return "", nil
		},
	}
	anchor, err := reviewmodel.NewLineAnchor("review.go", 3)
	require.NoError(t, err)
	wantDraft := reviewmodel.NewCommentDraft(0, anchor, "Use a constant.")

	worktree.
		EXPECT().
		CurrentBranch(gomock.Any()).
		Return("feature", nil)
	store.
		EXPECT().
		AddReviewDraft(gomock.Any(), "feature", wantDraft).
		Return(wantDraft.WithID(1), nil)

	err = handler.SaveCommentDraft(t.Context(), &CommentRequest{
		Anchor:  anchor,
		Message: "Use a constant.",
	})
	require.NoError(t, err)
}

func TestDraftHandler_SaveReplyDraft(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := NewMockStore(ctrl)
	handler := &DraftHandler{
		Log:      silog.Nop(),
		Worktree: NewMockWorktree(ctrl),
		Store:    store,
		Editor: func(context.Context, string) (string, error) {
			return "That makes sense.", nil
		},
	}
	wantDraft := reviewmodel.NewReplyDraft(0, "thread-1", "That makes sense.")

	store.
		EXPECT().
		AddReviewDraft(gomock.Any(), "feature", wantDraft).
		Return(wantDraft.WithID(2), nil)

	err := handler.SaveReplyDraft(t.Context(), &ReplyRequest{
		Branch:   "feature",
		ThreadID: "thread-1",
	})
	require.NoError(t, err)
}

func TestHandler_PostComment(t *testing.T) {
	ctrl := gomock.NewController(t)
	worktree := NewMockWorktree(ctrl)
	service := NewMockService(ctrl)
	repository := NewMockReviewRepository(ctrl)
	handler := &Handler{
		Log:        silog.Nop(),
		Worktree:   worktree,
		Service:    service,
		Store:      NewMockStore(ctrl),
		Repository: repository,
		Editor: func(context.Context, string) (string, error) {
			t.Fatal("editor should not open when a message is supplied")
			return "", nil
		},
	}
	anchor, err := reviewmodel.NewLineAnchor("review.go", 3)
	require.NoError(t, err)

	service.
		EXPECT().
		LookupBranch(gomock.Any(), "feature").
		Return(&spice.LookupBranchResponse{
			Base:   "main",
			Change: &testChangeMetadata{id: testChangeID("42")},
		}, nil)
	worktree.
		EXPECT().
		OpenBranchDiff(gomock.Any(), "main", "feature").
		Return(io.NopCloser(strings.NewReader(`diff --git a/review.go b/review.go
--- a/review.go
+++ b/review.go
@@ -1,2 +1,3 @@
 package review
+const answer = 42
 func check() {}
`)), nil)
	repository.
		EXPECT().
		SubmitReview(
			gomock.Any(),
			testChangeID("42"),
			forge.SubmitReviewRequest{
				Comments: []forge.SubmitReviewCommentRequest{
					{
						Path:  "review.go",
						Range: forge.ReviewThreadLine(3),
						Body:  "Use a constant.",
						Side:  forge.ReviewThreadSideRight,
					},
				},
			},
		).
		Return(forge.SubmitReviewResult{
			Comments: []forge.SubmitReviewCommentResult{
				{ThreadID: testThreadID("thread-1")},
			},
		}, nil)

	err = handler.PostComment(t.Context(), &CommentRequest{
		Branch:  "feature",
		Anchor:  anchor,
		Message: "Use a constant.",
	})
	require.NoError(t, err)
}

func TestHandler_PostReply(t *testing.T) {
	ctrl := gomock.NewController(t)
	service := NewMockService(ctrl)
	repository := NewMockReviewRepository(ctrl)
	handler := &Handler{
		Log:        silog.Nop(),
		Worktree:   NewMockWorktree(ctrl),
		Service:    service,
		Store:      NewMockStore(ctrl),
		Repository: repository,
		Editor: func(context.Context, string) (string, error) {
			return "Updated.", nil
		},
	}
	threadID := testThreadID("thread-1")

	service.
		EXPECT().
		LookupBranch(gomock.Any(), "feature").
		Return(&spice.LookupBranchResponse{
			Change: &testChangeMetadata{id: testChangeID("42")},
		}, nil)
	repository.
		EXPECT().
		ListReviewThreads(gomock.Any(), testChangeID("42")).
		Return(reviewThreadSeq(&forge.ReviewThread{ID: threadID}))
	repository.
		EXPECT().
		SubmitReview(
			gomock.Any(),
			testChangeID("42"),
			forge.SubmitReviewRequest{
				Comments: []forge.SubmitReviewCommentRequest{
					{ReplyTo: threadID, Body: "Updated."},
				},
			},
		).
		Return(forge.SubmitReviewResult{
			Comments: []forge.SubmitReviewCommentResult{
				{ThreadID: threadID},
			},
		}, nil)

	err := handler.PostReply(t.Context(), &ReplyRequest{
		Branch:   "feature",
		ThreadID: "thread-1",
	})
	require.NoError(t, err)
}

func TestDraftHandler_ReplaceDraftBody(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := NewMockStore(ctrl)
	handler := &DraftHandler{
		Log:      silog.Nop(),
		Worktree: NewMockWorktree(ctrl),
		Store:    store,
		Editor: func(context.Context, string) (string, error) {
			t.Fatal("editor should not open when a message is supplied")
			return "", nil
		},
	}
	anchor, err := reviewmodel.NewLineAnchor("review.go", 3)
	require.NoError(t, err)

	store.
		EXPECT().
		LoadReviewDrafts(gomock.Any(), "feature").
		Return([]Draft{
			reviewmodel.NewCommentDraft(2, anchor, "Old body."),
		}, nil)
	store.
		EXPECT().
		UpdateReviewDraftBody(gomock.Any(), "feature", DraftID(2), "New body.").
		Return(nil)

	err = handler.ReplaceDraftBody(
		t.Context(),
		&ReplaceDraftBodyRequest{
			Branch:  "feature",
			ID:      2,
			Message: "New body.",
		},
	)
	require.NoError(t, err)
}

func TestHandler_LoadReviewData(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := NewMockStore(ctrl)
	service := NewMockService(ctrl)
	repository := NewMockReviewRepository(ctrl)
	handler := &Handler{
		Log:        silog.Nop(),
		Worktree:   NewMockWorktree(ctrl),
		Service:    service,
		Store:      store,
		Repository: repository,
		Editor:     nil,
	}
	anchor, err := reviewmodel.NewLineAnchor("review.go", 3)
	require.NoError(t, err)
	draft := reviewmodel.NewCommentDraft(1, anchor, "Use a constant.")
	resolved := false
	thread := &forge.ReviewThread{
		ID:       testThreadID("thread-1"),
		Path:     "review.go",
		Range:    forge.ReviewThreadLine(3),
		Side:     forge.ReviewThreadSideRight,
		Resolved: &resolved,
		Comments: []forge.ReviewComment{
			{
				ID:     testCommentID("comment-1"),
				Body:   "Published.",
				Author: "reviewer",
			},
		},
	}

	store.
		EXPECT().
		LoadReviewDrafts(gomock.Any(), "feature").
		Return([]Draft{draft}, nil)
	service.
		EXPECT().
		LookupBranch(gomock.Any(), "feature").
		Return(&spice.LookupBranchResponse{
			Change: &testChangeMetadata{id: testChangeID("42")},
		}, nil)
	repository.
		EXPECT().
		ListReviewThreads(gomock.Any(), testChangeID("42")).
		Return(reviewThreadSeq(thread))

	got, err := handler.LoadReviewData(t.Context(), &LoadRequest{
		Branch: "feature",
	})
	require.NoError(t, err)
	assert.Equal(t, "feature", got.Branch)
	assert.Equal(t, []Draft{draft}, got.Drafts)
	assert.Equal(t, []ListedComment{
		{Thread: *thread, Comment: thread.Comments[0]},
	}, got.Comments)
}

func TestHandler_PublishDrafts(t *testing.T) {
	ctrl := gomock.NewController(t)
	worktree := NewMockWorktree(ctrl)
	store := NewMockStore(ctrl)
	service := NewMockService(ctrl)
	repository := NewMockReviewRepository(ctrl)
	handler := &Handler{
		Log:        silog.Nop(),
		Worktree:   worktree,
		Service:    service,
		Store:      store,
		Repository: repository,
		Editor:     nil,
	}
	anchor, err := reviewmodel.NewLineAnchor("review.go", 3)
	require.NoError(t, err)
	drafts := []Draft{
		reviewmodel.NewCommentDraft(1, anchor, "Use a constant."),
		reviewmodel.NewReplyDraft(2, "thread-1", "Updated."),
	}
	threadID := testThreadID("thread-1")

	store.
		EXPECT().
		LoadReviewDrafts(gomock.Any(), "feature").
		Return(drafts, nil)
	service.
		EXPECT().
		LookupBranch(gomock.Any(), "feature").
		Return(&spice.LookupBranchResponse{
			Base:   "main",
			Change: &testChangeMetadata{id: testChangeID("42")},
		}, nil)
	worktree.
		EXPECT().
		OpenBranchDiff(gomock.Any(), "main", "feature").
		Return(io.NopCloser(strings.NewReader(`diff --git a/review.go b/review.go
--- a/review.go
+++ b/review.go
@@ -1,2 +1,3 @@
 package review
+const answer = 42
 func check() {}
`)), nil)
	repository.
		EXPECT().
		ListReviewThreads(gomock.Any(), testChangeID("42")).
		Return(reviewThreadSeq(&forge.ReviewThread{ID: threadID}))
	repository.
		EXPECT().
		SubmitReview(
			gomock.Any(),
			testChangeID("42"),
			forge.SubmitReviewRequest{
				Body:        "Review body.",
				Disposition: forge.ReviewDispositionApprove,
				Comments: []forge.SubmitReviewCommentRequest{
					{
						Path:  "review.go",
						Range: forge.ReviewThreadLine(3),
						Body:  "Use a constant.",
						Side:  forge.ReviewThreadSideRight,
					},
					{ReplyTo: threadID, Body: "Updated."},
				},
			},
		).
		Return(forge.SubmitReviewResult{}, nil)
	store.
		EXPECT().
		ClearReviewDrafts(gomock.Any(), "feature").
		Return(nil)

	err = handler.PublishDrafts(t.Context(), &PublishDraftsRequest{
		Branch:      "feature",
		Body:        "Review body.",
		Disposition: forge.ReviewDispositionApprove,
	})
	require.NoError(t, err)
}

func TestThreadHandler_SetThreadResolution(t *testing.T) {
	ctrl := gomock.NewController(t)
	service := NewMockService(ctrl)
	repository := NewMockReviewRepository(ctrl)
	resolver := NewMockReviewThreadResolver(ctrl)
	handler := &ThreadHandler{
		Log:        silog.Nop(),
		Worktree:   NewMockWorktree(ctrl),
		Service:    service,
		Repository: repository,
		Resolver:   resolver,
	}
	threadID := testThreadID("thread-1")

	service.
		EXPECT().
		LookupBranch(gomock.Any(), "feature").
		Return(&spice.LookupBranchResponse{
			Change: &testChangeMetadata{id: testChangeID("42")},
		}, nil)
	repository.
		EXPECT().
		ListReviewThreads(gomock.Any(), testChangeID("42")).
		Return(reviewThreadSeq(&forge.ReviewThread{ID: threadID}))
	resolver.
		EXPECT().
		ResolveReviewThread(gomock.Any(), threadID).
		Return(nil)

	err := handler.SetThreadResolution(
		t.Context(),
		&SetThreadResolutionRequest{
			Branch:   "feature",
			ThreadID: "thread-1",
			Resolved: true,
		},
	)
	require.NoError(t, err)
}

func reviewThreadSeq(
	threads ...*forge.ReviewThread,
) iter.Seq2[*forge.ReviewThread, error] {
	return func(yield func(*forge.ReviewThread, error) bool) {
		for _, thread := range threads {
			if !yield(thread, nil) {
				return
			}
		}
	}
}

type testChangeMetadata struct {
	id         forge.ChangeID
	navigation forge.ChangeCommentID
}

func (*testChangeMetadata) ForgeID() string {
	return "test"
}

func (m *testChangeMetadata) ChangeID() forge.ChangeID {
	return m.id
}

func (m *testChangeMetadata) NavigationCommentID() forge.ChangeCommentID {
	return m.navigation
}

func (m *testChangeMetadata) SetNavigationCommentID(id forge.ChangeCommentID) {
	m.navigation = id
}

type testChangeID string

func (id testChangeID) String() string {
	return string(id)
}

type testThreadID string

func (id testThreadID) String() string {
	return string(id)
}

type testCommentID string

func (id testCommentID) String() string {
	return string(id)
}
