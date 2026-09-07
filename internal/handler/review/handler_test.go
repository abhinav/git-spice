package review

import (
	"bytes"
	"context"
	"errors"
	"io"
	"iter"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	reviewmodel "go.abhg.dev/gs/internal/review"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.uber.org/mock/gomock"
)

func TestDraftHandler_SaveCommentDraft(t *testing.T) {
	ctrl := gomock.NewController(t)
	service := NewMockService(ctrl)
	store := NewMockStore(ctrl)
	handler := &DraftHandler{
		Log:     silog.Nop(),
		Service: service,
		Store:   store,
		Editor: func(context.Context, string) (string, error) {
			t.Fatal("editor should not open when a message is supplied")
			return "", nil
		},
	}
	anchor := reviewmodel.Anchor{Path: "review.go", StartLine: 3, EndLine: 3}
	head := git.Hash("1111111111111111111111111111111111111111")
	wantDraft := reviewmodel.Draft{
		ID:         0,
		Body:       "Use a constant.",
		Anchor:     anchor,
		CommitHash: head,
	}
	wantSavedDraft := wantDraft
	wantSavedDraft.ID = 1

	service.
		EXPECT().
		LookupBranch(gomock.Any(), "feature").
		Return(&spice.LookupBranchResponse{Head: head}, nil)
	store.
		EXPECT().
		AddReviewDraft(gomock.Any(), "feature", wantDraft).
		Return(wantSavedDraft, nil)

	err := handler.SaveCommentDraft(t.Context(), &CommentRequest{
		Branch:  "feature",
		Anchor:  anchor,
		Message: "Use a constant.",
	})
	require.NoError(t, err)
}

func TestDraftHandler_SaveCommentDraft_capturesHeadBeforeEditor(t *testing.T) {
	ctrl := gomock.NewController(t)
	service := NewMockService(ctrl)
	store := NewMockStore(ctrl)
	sourceHead := git.Hash("1111111111111111111111111111111111111111")
	updatedHead := git.Hash("2222222222222222222222222222222222222222")
	currentHead := sourceHead
	anchor := reviewmodel.Anchor{Path: "review.go", StartLine: 3, EndLine: 3}
	handler := &DraftHandler{
		Log:     silog.Nop(),
		Service: service,
		Store:   store,
		Editor: func(context.Context, string) (string, error) {
			currentHead = updatedHead
			return "Use a constant.", nil
		},
	}

	service.
		EXPECT().
		LookupBranch(gomock.Any(), "feature").
		DoAndReturn(func(context.Context, string) (*spice.LookupBranchResponse, error) {
			return &spice.LookupBranchResponse{Head: currentHead}, nil
		})
	store.
		EXPECT().
		AddReviewDraft(
			gomock.Any(),
			"feature",
			reviewmodel.Draft{
				ID:         0,
				Body:       "Use a constant.",
				Anchor:     anchor,
				CommitHash: sourceHead,
			},
		).
		Return(reviewmodel.Draft{
			ID:         1,
			Body:       "Use a constant.",
			Anchor:     anchor,
			CommitHash: sourceHead,
		}, nil)

	err := handler.SaveCommentDraft(t.Context(), &CommentRequest{
		Branch: "feature",
		Anchor: anchor,
	})
	require.NoError(t, err)
}

func TestDraftHandler_SaveReplyDraft(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := NewMockStore(ctrl)
	handler := &DraftHandler{
		Log:     silog.Nop(),
		Service: nil,
		Store:   store,
		Editor: func(context.Context, string) (string, error) {
			return "That makes sense.", nil
		},
	}
	wantDraft := reviewmodel.Draft{
		ID:      0,
		Body:    "That makes sense.",
		ReplyTo: "thread-1",
	}
	wantSavedDraft := wantDraft
	wantSavedDraft.ID = 2

	store.
		EXPECT().
		AddReviewDraft(gomock.Any(), "feature", wantDraft).
		Return(wantSavedDraft, nil)

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
	anchor := reviewmodel.Anchor{Path: "review.go", StartLine: 3, EndLine: 3}

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

	err := handler.PostComment(t.Context(), &CommentRequest{
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
		Log:     silog.Nop(),
		Service: nil,
		Store:   store,
		Editor: func(context.Context, string) (string, error) {
			t.Fatal("editor should not open when a message is supplied")
			return "", nil
		},
	}
	anchor := reviewmodel.Anchor{Path: "review.go", StartLine: 3, EndLine: 3}

	store.
		EXPECT().
		LoadReviewDrafts(gomock.Any(), "feature").
		Return([]Draft{
			{ID: 2, Body: "Old body.", Anchor: anchor},
		}, nil)
	store.
		EXPECT().
		UpdateReviewDraftBody(gomock.Any(), "feature", DraftID(2), "New body.").
		Return(nil)

	err := handler.ReplaceDraftBody(
		t.Context(),
		&ReplaceDraftBodyRequest{
			Branch:  "feature",
			ID:      2,
			Message: "New body.",
		},
	)
	require.NoError(t, err)
}

func TestDraftHandler_DeleteDrafts(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := NewMockStore(ctrl)
	handler := &DraftHandler{
		Log:     silog.Nop(),
		Service: nil,
		Store:   store,
		Editor:  nil,
	}

	store.
		EXPECT().
		DeleteReviewDrafts(
			gomock.Any(),
			"feature",
			[]DraftID{1, 3},
		).
		Return(2, nil)

	err := handler.DeleteDrafts(t.Context(), &DeleteDraftsRequest{
		Branch: "feature",
		IDs:    []DraftID{1, 3},
	})
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
	anchor := reviewmodel.Anchor{Path: "review.go", StartLine: 3, EndLine: 3}
	draft := reviewmodel.Draft{
		ID:     1,
		Body:   "Use a constant.",
		Anchor: anchor,
	}
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
	anchor := reviewmodel.Anchor{Path: "review.go", StartLine: 3, EndLine: 3}
	head := git.Hash("2222222222222222222222222222222222222222")
	drafts := []Draft{
		{ID: 1, Body: "Use a constant.", Anchor: anchor, CommitHash: head},
		{ID: 2, Body: "Updated.", ReplyTo: "thread-1"},
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
			Head:   head,
			Change: &testChangeMetadata{id: testChangeID("42")},
		}, nil)
	repository.
		EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{testChangeID("42")}).
		Return([]forge.ChangeStatus{{HeadHash: head}}, nil)
	worktree.
		EXPECT().
		PeelToCommit(gomock.Any(), head.String()).
		Return(head, nil)
	worktree.
		EXPECT().
		OpenBranchDiff(gomock.Any(), "main", head.String()).
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
		RemovePublishedReviewDrafts(gomock.Any(), "feature", drafts).
		Return(nil)

	err := handler.PublishDrafts(t.Context(), &PublishDraftsRequest{
		Branch:      "feature",
		Body:        "Review body.",
		Disposition: forge.ReviewDispositionApprove,
	})
	require.NoError(t, err)
}

func TestHandler_PublishDrafts_remapsAnchorsOncePerSource(t *testing.T) {
	ctrl := gomock.NewController(t)
	worktree := NewMockWorktree(ctrl)
	store := NewMockStore(ctrl)
	service := NewMockService(ctrl)
	repository := NewMockReviewRepository(ctrl)
	var logBuffer bytes.Buffer
	handler := &Handler{
		Log:        silog.New(&logBuffer, nil),
		Worktree:   worktree,
		Service:    service,
		Store:      store,
		Repository: repository,
		Editor:     nil,
	}
	source := git.Hash("1111111111111111111111111111111111111111")
	target := git.Hash("2222222222222222222222222222222222222222")
	localHead := git.Hash("3333333333333333333333333333333333333333")
	drafts := []Draft{
		{
			ID:         1,
			Body:       "Use a constant.",
			Anchor:     reviewmodel.Anchor{Path: "old.go", StartLine: 2, EndLine: 2},
			CommitHash: source,
		},
		{
			ID:         2,
			Body:       "Keep this name.",
			Anchor:     reviewmodel.Anchor{Path: "other.go", StartLine: 1, EndLine: 1},
			CommitHash: source,
		},
	}
	store.
		EXPECT().
		LoadReviewDrafts(gomock.Any(), "feature").
		Return(drafts, nil)
	service.
		EXPECT().
		LookupBranch(gomock.Any(), "feature").
		Return(&spice.LookupBranchResponse{
			Base:   "main",
			Head:   localHead,
			Change: &testChangeMetadata{id: testChangeID("42")},
		}, nil)
	repository.
		EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{testChangeID("42")}).
		Return([]forge.ChangeStatus{{HeadHash: target}}, nil)
	worktree.
		EXPECT().
		PeelToCommit(gomock.Any(), target.String()).
		Return(target, nil)
	worktree.
		EXPECT().
		OpenBranchDiff(gomock.Any(), "main", target.String()).
		Return(io.NopCloser(strings.NewReader(`diff --git a/new.go b/new.go
new file mode 100644
--- /dev/null
+++ b/new.go
@@ -0,0 +1,3 @@
+zero
+one
+two
diff --git a/other.go b/other.go
new file mode 100644
--- /dev/null
+++ b/other.go
@@ -0,0 +1 @@
+package other
`)), nil)
	worktree.
		EXPECT().
		PeelToCommit(gomock.Any(), source.String()).
		Return(source, nil)
	worktree.
		EXPECT().
		OpenCommitDiff(gomock.Any(), source.String(), target.String()).
		Return(io.NopCloser(strings.NewReader(`diff --git a/old.go b/new.go
similarity index 66%
rename from old.go
rename to new.go
--- a/old.go
+++ b/new.go
@@ -1,2 +1,3 @@
+zero
 one
 two
`)), nil)
	repository.
		EXPECT().
		SubmitReview(
			gomock.Any(),
			testChangeID("42"),
			forge.SubmitReviewRequest{
				Comments: []forge.SubmitReviewCommentRequest{
					{
						Path:  "new.go",
						Range: forge.ReviewThreadLine(3),
						Body:  "Use a constant.",
						Side:  forge.ReviewThreadSideRight,
					},
					{
						Path:  "other.go",
						Range: forge.ReviewThreadLine(1),
						Body:  "Keep this name.",
						Side:  forge.ReviewThreadSideRight,
					},
				},
			},
		).
		Return(forge.SubmitReviewResult{}, nil)
	store.
		EXPECT().
		RemovePublishedReviewDrafts(gomock.Any(), "feature", drafts).
		Return(nil)

	err := handler.PublishDrafts(t.Context(), &PublishDraftsRequest{
		Branch: "feature",
	})
	require.NoError(t, err)
	assert.Contains(
		t,
		logBuffer.String(),
		"Draft 1: anchor moved to new.go:3",
	)
	assert.NotContains(t, logBuffer.String(), "Draft 2: anchor moved")
}

func TestHandler_PublishDrafts_statusFailureUsesLocalHead(t *testing.T) {
	ctrl := gomock.NewController(t)
	worktree := NewMockWorktree(ctrl)
	store := NewMockStore(ctrl)
	service := NewMockService(ctrl)
	repository := NewMockReviewRepository(ctrl)
	var logBuffer bytes.Buffer
	handler := &Handler{
		Log:        silog.New(&logBuffer, nil),
		Worktree:   worktree,
		Service:    service,
		Store:      store,
		Repository: repository,
		Editor:     nil,
	}
	localHead := git.Hash("1111111111111111111111111111111111111111")
	drafts := []Draft{{
		ID:         1,
		Body:       "Use a constant.",
		Anchor:     reviewmodel.Anchor{Path: "main.go", StartLine: 2, EndLine: 2},
		CommitHash: localHead,
	}}

	store.
		EXPECT().
		LoadReviewDrafts(gomock.Any(), "feature").
		Return(drafts, nil)
	service.
		EXPECT().
		LookupBranch(gomock.Any(), "feature").
		Return(&spice.LookupBranchResponse{
			Base:   "main",
			Head:   localHead,
			Change: &testChangeMetadata{id: testChangeID("42")},
		}, nil)
	repository.
		EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{testChangeID("42")}).
		Return(nil, errors.New("status unavailable"))
	worktree.
		EXPECT().
		PeelToCommit(gomock.Any(), localHead.String()).
		Return(localHead, nil)
	worktree.
		EXPECT().
		OpenBranchDiff(gomock.Any(), "main", localHead.String()).
		Return(io.NopCloser(strings.NewReader(`diff --git a/main.go b/main.go
new file mode 100644
--- /dev/null
+++ b/main.go
@@ -0,0 +1,2 @@
+package main
+const answer = 42
`)), nil)
	repository.
		EXPECT().
		SubmitReview(
			gomock.Any(),
			testChangeID("42"),
			forge.SubmitReviewRequest{
				Comments: []forge.SubmitReviewCommentRequest{{
					Path:  "main.go",
					Range: forge.ReviewThreadLine(2),
					Body:  "Use a constant.",
					Side:  forge.ReviewThreadSideRight,
				}},
			},
		).
		Return(forge.SubmitReviewResult{}, nil)
	store.
		EXPECT().
		RemovePublishedReviewDrafts(gomock.Any(), "feature", drafts).
		Return(nil)

	err := handler.PublishDrafts(t.Context(), &PublishDraftsRequest{
		Branch: "feature",
	})
	require.NoError(t, err)
	assert.Contains(t, logBuffer.String(), "using local branch head")
	assert.Contains(t, logBuffer.String(), "status unavailable")
}

func TestHandler_PublishDrafts_missingRemoteHeadUsesLocalHead(t *testing.T) {
	ctrl := gomock.NewController(t)
	worktree := NewMockWorktree(ctrl)
	store := NewMockStore(ctrl)
	service := NewMockService(ctrl)
	repository := NewMockReviewRepository(ctrl)
	var logBuffer bytes.Buffer
	handler := &Handler{
		Log:        silog.New(&logBuffer, nil),
		Worktree:   worktree,
		Service:    service,
		Store:      store,
		Repository: repository,
		Editor:     nil,
	}
	localHead := git.Hash("1111111111111111111111111111111111111111")
	drafts := []Draft{{
		ID:         1,
		Body:       "Use a constant.",
		Anchor:     reviewmodel.Anchor{Path: "main.go", StartLine: 2, EndLine: 2},
		CommitHash: localHead,
	}}

	store.
		EXPECT().
		LoadReviewDrafts(gomock.Any(), "feature").
		Return(drafts, nil)
	service.
		EXPECT().
		LookupBranch(gomock.Any(), "feature").
		Return(&spice.LookupBranchResponse{
			Base:   "main",
			Head:   localHead,
			Change: &testChangeMetadata{id: testChangeID("42")},
		}, nil)
	repository.
		EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{testChangeID("42")}).
		Return([]forge.ChangeStatus{{}}, nil)
	worktree.
		EXPECT().
		PeelToCommit(gomock.Any(), localHead.String()).
		Return(localHead, nil)
	worktree.
		EXPECT().
		OpenBranchDiff(gomock.Any(), "main", localHead.String()).
		Return(io.NopCloser(strings.NewReader(`diff --git a/main.go b/main.go
new file mode 100644
--- /dev/null
+++ b/main.go
@@ -0,0 +1,2 @@
+package main
+const answer = 42
`)), nil)
	repository.
		EXPECT().
		SubmitReview(
			gomock.Any(),
			testChangeID("42"),
			forge.SubmitReviewRequest{
				Comments: []forge.SubmitReviewCommentRequest{{
					Path:  "main.go",
					Range: forge.ReviewThreadLine(2),
					Body:  "Use a constant.",
					Side:  forge.ReviewThreadSideRight,
				}},
			},
		).
		Return(forge.SubmitReviewResult{}, nil)
	store.
		EXPECT().
		RemovePublishedReviewDrafts(gomock.Any(), "feature", drafts).
		Return(nil)

	err := handler.PublishDrafts(t.Context(), &PublishDraftsRequest{
		Branch: "feature",
	})
	require.NoError(t, err)
	assert.Contains(t, logBuffer.String(), "using local branch head")
}

func TestHandler_PublishDrafts_unavailableHeadUsesSavedAnchor(t *testing.T) {
	ctrl := gomock.NewController(t)
	worktree := NewMockWorktree(ctrl)
	store := NewMockStore(ctrl)
	service := NewMockService(ctrl)
	repository := NewMockReviewRepository(ctrl)
	var logBuffer bytes.Buffer
	handler := &Handler{
		Log:        silog.New(&logBuffer, nil),
		Worktree:   worktree,
		Service:    service,
		Store:      store,
		Repository: repository,
		Editor:     nil,
	}
	sourceHead := git.Hash("1111111111111111111111111111111111111111")
	remoteHead := git.Hash("2222222222222222222222222222222222222222")
	drafts := []Draft{{
		ID:         1,
		Body:       "Use a constant.",
		Anchor:     reviewmodel.Anchor{Path: "main.go", StartLine: 2, EndLine: 2},
		CommitHash: sourceHead,
	}}

	store.
		EXPECT().
		LoadReviewDrafts(gomock.Any(), "feature").
		Return(drafts, nil)
	service.
		EXPECT().
		LookupBranch(gomock.Any(), "feature").
		Return(&spice.LookupBranchResponse{
			Base:   "main",
			Head:   sourceHead,
			Change: &testChangeMetadata{id: testChangeID("42")},
		}, nil)
	repository.
		EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{testChangeID("42")}).
		Return([]forge.ChangeStatus{{HeadHash: remoteHead}}, nil)
	worktree.
		EXPECT().
		PeelToCommit(gomock.Any(), remoteHead.String()).
		Return("", git.ErrNotExist)
	worktree.
		EXPECT().
		OpenBranchDiff(gomock.Any(), "main", "feature").
		Return(io.NopCloser(strings.NewReader(`diff --git a/main.go b/main.go
new file mode 100644
--- /dev/null
+++ b/main.go
@@ -0,0 +1,2 @@
+package main
+const answer = 42
`)), nil)
	repository.
		EXPECT().
		SubmitReview(
			gomock.Any(),
			testChangeID("42"),
			forge.SubmitReviewRequest{
				Comments: []forge.SubmitReviewCommentRequest{{
					Path:  "main.go",
					Range: forge.ReviewThreadLine(2),
					Body:  "Use a constant.",
					Side:  forge.ReviewThreadSideRight,
				}},
			},
		).
		Return(forge.SubmitReviewResult{}, nil)
	store.
		EXPECT().
		RemovePublishedReviewDrafts(gomock.Any(), "feature", drafts).
		Return(nil)

	err := handler.PublishDrafts(t.Context(), &PublishDraftsRequest{
		Branch: "feature",
	})
	require.NoError(t, err)
	assert.Contains(t, logBuffer.String(), "using saved anchors")
	assert.Contains(t, logBuffer.String(), git.ErrNotExist.Error())
}

func TestHandler_remapDraftAnchors_stopsAfterDeletedAnchor(t *testing.T) {
	ctrl := gomock.NewController(t)
	worktree := NewMockWorktree(ctrl)
	handler := &Handler{
		Log:        silog.Nop(),
		Worktree:   worktree,
		Service:    nil,
		Store:      nil,
		Repository: nil,
		Editor:     nil,
	}
	firstSource := git.Hash("1111111111111111111111111111111111111111")
	secondSource := git.Hash("2222222222222222222222222222222222222222")
	head := git.Hash("3333333333333333333333333333333333333333")
	drafts := []Draft{
		{
			ID:         1,
			Body:       "Deleted target.",
			Anchor:     reviewmodel.Anchor{Path: "deleted.go", StartLine: 1, EndLine: 1},
			CommitHash: firstSource,
		},
		{
			ID:         2,
			Body:       "Later target.",
			Anchor:     reviewmodel.Anchor{Path: "later.go", StartLine: 1, EndLine: 1},
			CommitHash: secondSource,
		},
	}

	worktree.
		EXPECT().
		PeelToCommit(gomock.Any(), firstSource.String()).
		Return(firstSource, nil)
	worktree.
		EXPECT().
		OpenCommitDiff(gomock.Any(), firstSource.String(), head.String()).
		Return(io.NopCloser(strings.NewReader(`diff --git a/deleted.go b/deleted.go
deleted file mode 100644
--- a/deleted.go
+++ /dev/null
@@ -1 +0,0 @@
-package deleted
`)), nil)

	err := handler.remapDraftAnchors(t.Context(), head, drafts)
	assert.ErrorContains(t, err, "comment target deleted.go:1 no longer exists")
}

func TestHandler_remapDraftAnchors_sourceDiffFailureUsesSavedAnchor(
	t *testing.T,
) {
	ctrl := gomock.NewController(t)
	worktree := NewMockWorktree(ctrl)
	var logBuffer bytes.Buffer
	handler := &Handler{
		Log:        silog.New(&logBuffer, nil),
		Worktree:   worktree,
		Service:    nil,
		Store:      nil,
		Repository: nil,
		Editor:     nil,
	}
	source := git.Hash("1111111111111111111111111111111111111111")
	head := git.Hash("2222222222222222222222222222222222222222")
	draft := Draft{
		ID:         1,
		Body:       "Use a constant.",
		Anchor:     reviewmodel.Anchor{Path: "main.go", StartLine: 2, EndLine: 2},
		CommitHash: source,
	}

	worktree.
		EXPECT().
		PeelToCommit(gomock.Any(), source.String()).
		Return(source, nil)
	worktree.
		EXPECT().
		OpenCommitDiff(gomock.Any(), source.String(), head.String()).
		Return(nil, errors.New("diff unavailable"))

	err := handler.remapDraftAnchors(t.Context(), head, []Draft{draft})
	require.NoError(t, err)
	assert.Contains(t, logBuffer.String(), "Using saved anchor")
	assert.Contains(t, logBuffer.String(), "diff unavailable")
}

func TestHandler_PublishDrafts_unavailableSourceUsesSavedAnchor(t *testing.T) {
	ctrl := gomock.NewController(t)
	worktree := NewMockWorktree(ctrl)
	store := NewMockStore(ctrl)
	service := NewMockService(ctrl)
	repository := NewMockReviewRepository(ctrl)
	var logBuffer bytes.Buffer
	handler := &Handler{
		Log:        silog.New(&logBuffer, nil),
		Worktree:   worktree,
		Service:    service,
		Store:      store,
		Repository: repository,
		Editor:     nil,
	}
	source := git.Hash("1111111111111111111111111111111111111111")
	target := git.Hash("2222222222222222222222222222222222222222")
	drafts := []Draft{{
		ID:         1,
		Body:       "Use a constant.",
		Anchor:     reviewmodel.Anchor{Path: "main.go", StartLine: 2, EndLine: 2},
		CommitHash: source,
	}}

	store.
		EXPECT().
		LoadReviewDrafts(gomock.Any(), "feature").
		Return(drafts, nil)
	service.
		EXPECT().
		LookupBranch(gomock.Any(), "feature").
		Return(&spice.LookupBranchResponse{
			Base:   "main",
			Head:   target,
			Change: &testChangeMetadata{id: testChangeID("42")},
		}, nil)
	repository.
		EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{testChangeID("42")}).
		Return([]forge.ChangeStatus{{HeadHash: target}}, nil)
	worktree.
		EXPECT().
		PeelToCommit(gomock.Any(), target.String()).
		Return(target, nil)
	worktree.
		EXPECT().
		OpenBranchDiff(gomock.Any(), "main", target.String()).
		Return(io.NopCloser(strings.NewReader(`diff --git a/main.go b/main.go
new file mode 100644
--- /dev/null
+++ b/main.go
@@ -0,0 +1,2 @@
+package main
+const answer = 42
`)), nil)
	worktree.
		EXPECT().
		PeelToCommit(gomock.Any(), source.String()).
		Return("", git.ErrNotExist)
	repository.
		EXPECT().
		SubmitReview(
			gomock.Any(),
			testChangeID("42"),
			forge.SubmitReviewRequest{
				Comments: []forge.SubmitReviewCommentRequest{{
					Path:  "main.go",
					Range: forge.ReviewThreadLine(2),
					Body:  "Use a constant.",
					Side:  forge.ReviewThreadSideRight,
				}},
			},
		).
		Return(forge.SubmitReviewResult{}, nil)
	store.
		EXPECT().
		RemovePublishedReviewDrafts(gomock.Any(), "feature", drafts).
		Return(nil)

	err := handler.PublishDrafts(t.Context(), &PublishDraftsRequest{
		Branch: "feature",
	})
	require.NoError(t, err)
	assert.Contains(t, logBuffer.String(), "Using saved anchor")
	assert.Contains(t, logBuffer.String(), "source commit 1111111")
}

func TestThreadHandler_SetThreadResolution(t *testing.T) {
	ctrl := gomock.NewController(t)
	service := NewMockService(ctrl)
	repository := NewMockReviewRepository(ctrl)
	resolver := NewMockReviewThreadResolver(ctrl)
	handler := &ThreadHandler{
		Log:        silog.Nop(),
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
