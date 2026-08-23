package gitlab

import (
	"crypto/sha1" // #nosec G505 -- GitLab defines line codes with SHA-1.
	json "encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	gitlabgateway "go.abhg.dev/gs/internal/gateway/gitlab"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/sliceutil"
)

func TestMRDiscussion_String(t *testing.T) {
	id := &MRDiscussion{
		DiscussionID: "discussion:with:colons",
		MRNumber:     55,
	}
	assert.Equal(t, "discussion:with:colons:55", id.String())
}

func TestMRReviewComment_String(t *testing.T) {
	id := &MRReviewComment{
		DiscussionID: "discussion:with:colons",
		NoteID:       88,
		MRNumber:     55,
	}
	assert.Equal(t, "discussion:with:colons:88:55", id.String())
}

func TestRepository_ListReviewerStates_onlyEffectiveDispositions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v4/projects/42/merge_requests/55/reviewers", r.URL.Path)
		writeJSON(t, w, []*gitlabgateway.MergeRequestReviewer{
			{User: gitlabgateway.BasicUser{Username: "unassigned"}, State: gitlabgateway.ReviewerStateUnreviewed},
			{User: gitlabgateway.BasicUser{Username: "working"}, State: gitlabgateway.ReviewerStateReviewStarted},
			{User: gitlabgateway.BasicUser{Username: "commenter"}, State: gitlabgateway.ReviewerStateReviewed},
			{User: gitlabgateway.BasicUser{Username: "blocker"}, State: gitlabgateway.ReviewerStateRequestedChanges},
			{User: gitlabgateway.BasicUser{Username: "approver"}, State: gitlabgateway.ReviewerStateApproved},
			{User: gitlabgateway.BasicUser{Username: "former-approver"}, State: gitlabgateway.ReviewerStateUnapproved},
		})
	}))
	defer srv.Close()

	repo := newReviewTestRepository(t, srv)
	states, err := sliceutil.CollectErr(
		repo.ListReviewerStates(t.Context(), &MR{Number: 55}))
	require.NoError(t, err)
	assert.Equal(t, []*forge.ReviewerState{
		{Reviewer: "blocker", Disposition: forge.ReviewDispositionRequestChanges},
		{Reviewer: "approver", Disposition: forge.ReviewDispositionApprove},
	}, states)
}

func TestRepository_ListReviewThreads(t *testing.T) {
	createdAt := time.Date(2026, time.August, 22, 12, 30, 0, 0, time.UTC)
	var page int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v4/projects/42/merge_requests/55/discussions", r.URL.Path)
		assert.Equal(t, "100", r.URL.Query().Get("per_page"))
		page++
		switch page {
		case 1:
			assert.Empty(t, r.URL.Query().Get("page"))
			w.Header().Set("X-Next-Page", "2")
			writeJSON(t, w, []*gitlabgateway.Discussion{
				{
					ID: "right-range",
					Notes: []*gitlabgateway.DiscussionNote{
						{
							ID:         81,
							Body:       "Range root.",
							Author:     gitlabgateway.DiscussionNoteUser{Username: "spock"},
							CreatedAt:  &createdAt,
							Resolvable: true,
							Resolved:   true,
							Position: &gitlabgateway.DiscussionPosition{
								PositionType: "text",
								NewPath:      "new/name.go",
								OldPath:      "old/name.go",
								NewLine:      12,
								LineRange: &gitlabgateway.LineRange{
									Start: gitlabgateway.LinePosition{Type: "new", NewLine: 10},
									End:   gitlabgateway.LinePosition{Type: "new", NewLine: 12},
								},
							},
						},
						{ID: 82, Body: "Range reply.", Author: gitlabgateway.DiscussionNoteUser{Username: "scotty"}},
					},
				},
				{ID: "overview", Notes: []*gitlabgateway.DiscussionNote{{ID: 90}}},
			})
		case 2:
			assert.Equal(t, "2", r.URL.Query().Get("page"))
			writeJSON(t, w, []*gitlabgateway.Discussion{
				{
					ID: "left-line",
					Notes: []*gitlabgateway.DiscussionNote{
						{
							ID:         83,
							Body:       "Removed line.",
							Resolvable: true,
							Position: &gitlabgateway.DiscussionPosition{
								PositionType: "text",
								NewPath:      "new/name.go",
								OldPath:      "old/name.go",
								OldLine:      8,
							},
						},
					},
				},
				{
					ID: "file",
					Notes: []*gitlabgateway.DiscussionNote{
						{
							ID:         84,
							Body:       "Whole file.",
							Resolvable: true,
							Position: &gitlabgateway.DiscussionPosition{
								PositionType: "file",
								NewPath:      "new/file.go",
								OldPath:      "old/file.go",
							},
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected page %d", page)
		}
	}))
	defer srv.Close()

	repo := newReviewTestRepository(t, srv)
	threads, err := sliceutil.CollectErr(
		repo.ListReviewThreads(t.Context(), &MR{Number: 55}))
	require.NoError(t, err)
	require.Len(t, threads, 3)

	assert.Equal(t, &MRDiscussion{DiscussionID: "right-range", MRNumber: 55}, threads[0].ID)
	assert.Equal(t, "new/name.go", threads[0].Path)
	assert.Equal(t, forge.ReviewThreadRange{StartLine: 10, EndLine: 12}, threads[0].Range)
	assert.Equal(t, forge.ReviewThreadSideRight, threads[0].Side)
	require.NotNil(t, threads[0].Resolved)
	assert.True(t, *threads[0].Resolved)
	assert.Nil(t, threads[0].Outdated)
	assert.Equal(t, []forge.ReviewComment{
		{ID: &MRReviewComment{DiscussionID: "right-range", NoteID: 81, MRNumber: 55}, Body: "Range root.", Author: "spock", CreatedAt: createdAt},
		{ID: &MRReviewComment{DiscussionID: "right-range", NoteID: 82, MRNumber: 55}, Body: "Range reply.", Author: "scotty"},
	}, threads[0].Comments)

	assert.Equal(t, &MRDiscussion{DiscussionID: "left-line", MRNumber: 55}, threads[1].ID)
	assert.Equal(t, "old/name.go", threads[1].Path)
	assert.Equal(t, forge.ReviewThreadLine(8), threads[1].Range)
	assert.Equal(t, forge.ReviewThreadSideLeft, threads[1].Side)
	require.NotNil(t, threads[1].Resolved)
	assert.False(t, *threads[1].Resolved)

	assert.Equal(t, &MRDiscussion{DiscussionID: "file", MRNumber: 55}, threads[2].ID)
	assert.Equal(t, "new/file.go", threads[2].Path)
	assert.True(t, threads[2].Range.IsZero())
}

func TestReviewThreadPosition_textWithoutLine(t *testing.T) {
	_, _, _, err := reviewThreadPosition(&gitlabgateway.DiscussionPosition{
		PositionType: "text",
		NewPath:      "new/name.go",
		OldPath:      "old/name.go",
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "position has no line")
}

func TestRepository_SubmitReview(t *testing.T) {
	var (
		discussions []gitlabgateway.CreateMergeRequestDiscussionOptions
		summary     gitlabgateway.CreateMergeRequestNoteOptions
		replied     bool
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/api/v4/projects/42/merge_requests/55":
			assert.Equal(t, http.MethodGet, r.Method)
			writeJSON(t, w, gitlabgateway.MergeRequest{
				DiffRefs: gitlabgateway.MergeRequestDiffRefs{
					BaseSHA: "base", HeadSHA: "head", StartSHA: "start",
				},
			})
		case "/api/v4/projects/42/merge_requests/55/discussions":
			assert.Equal(t, http.MethodPost, r.Method)
			var req gitlabgateway.CreateMergeRequestDiscussionOptions
			require.NoError(t, decodeJSON(r, &req))
			discussions = append(discussions, req)
			n := int64(100 + len(discussions))
			writeJSON(t, w, gitlabgateway.Discussion{
				ID:    fmt.Sprintf("discussion-%d", len(discussions)),
				Notes: []*gitlabgateway.DiscussionNote{{ID: n}},
			})
		case "/api/v4/projects/42/merge_requests/55/discussions/existing%2Fthread/notes":
			assert.Equal(t, http.MethodPost, r.Method)
			assert.False(t, replied)
			replied = true
			var req gitlabgateway.AddMergeRequestDiscussionNoteOptions
			require.NoError(t, decodeJSON(r, &req))
			require.NotNil(t, req.Body)
			assert.Equal(t, "Reply.", *req.Body)
			writeJSON(t, w, gitlabgateway.DiscussionNote{ID: 104})
		case "/api/v4/projects/42/merge_requests/55/notes":
			assert.Equal(t, http.MethodPost, r.Method)
			require.NoError(t, decodeJSON(r, &summary))
			writeJSON(t, w, gitlabgateway.Note{ID: 104})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.EscapedPath())
		}
	}))
	defer srv.Close()

	repo := newReviewTestRepository(t, srv)
	existing := &MRDiscussion{DiscussionID: "existing/thread", MRNumber: 55}
	result, err := repo.SubmitReview(
		t.Context(),
		&MR{Number: 55},
		forge.SubmitReviewRequest{
			Body:        "Review summary.",
			Disposition: forge.ReviewDispositionRequestChanges,
			Comments: []forge.SubmitReviewCommentRequest{
				{
					Path:  "new.go",
					Range: forge.ReviewThreadRange{StartLine: 10, EndLine: 12},
					Side:  forge.ReviewThreadSideRight,
					Body:  "Range.",
				},
				{
					Path:  "old.go",
					Range: forge.ReviewThreadLine(8),
					Side:  forge.ReviewThreadSideLeft,
					Body:  "Removed.",
				},
				{
					Path: "file.go",
					Side: forge.ReviewThreadSide(99),
					Body: "Whole file.",
				},
				{ReplyTo: existing, Body: "Reply."},
			},
		},
	)
	require.NoError(t, err)
	assert.Equal(t, forge.SubmitReviewResult{
		Comments: []forge.SubmitReviewCommentResult{
			{ThreadID: &MRDiscussion{DiscussionID: "discussion-1", MRNumber: 55}, CommentID: &MRReviewComment{DiscussionID: "discussion-1", NoteID: 101, MRNumber: 55}},
			{ThreadID: &MRDiscussion{DiscussionID: "discussion-2", MRNumber: 55}, CommentID: &MRReviewComment{DiscussionID: "discussion-2", NoteID: 102, MRNumber: 55}},
			{ThreadID: &MRDiscussion{DiscussionID: "discussion-3", MRNumber: 55}, CommentID: &MRReviewComment{DiscussionID: "discussion-3", NoteID: 103, MRNumber: 55}},
			{ThreadID: existing, CommentID: &MRReviewComment{DiscussionID: "existing/thread", NoteID: 104, MRNumber: 55}},
		},
	}, result)
	require.Len(t, discussions, 3)
	assertRightRangePosition(t, discussions[0].Position, "new.go", 10, 12)
	assertLeftLinePosition(t, discussions[1].Position, "old.go", 8)
	assertFilePosition(t, discussions[2].Position, "file.go")
	require.NotNil(t, summary.Body)
	assert.Equal(t, "Review summary.", *summary.Body)
}

func TestRepository_SubmitReview_approve(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/api/v4/projects/42/merge_requests/55/notes":
			var req gitlabgateway.CreateMergeRequestNoteOptions
			require.NoError(t, decodeJSON(r, &req))
			require.NotNil(t, req.Body)
			assert.Equal(t, "Approved.", *req.Body)
			writeJSON(t, w, gitlabgateway.Note{ID: 90})
		case "/api/v4/projects/42/merge_requests/55/approve":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	repo := newReviewTestRepository(t, srv)
	result, err := repo.SubmitReview(t.Context(), &MR{Number: 55}, forge.SubmitReviewRequest{
		Body:        "Approved.",
		Disposition: forge.ReviewDispositionApprove,
	})
	require.NoError(t, err)
	assert.Empty(t, result.Comments)
	assert.Equal(t, []string{
		"/api/v4/projects/42/merge_requests/55/notes",
		"/api/v4/projects/42/merge_requests/55/approve",
	}, paths)
}

func TestRepository_SubmitReview_requestChangesFallback(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v4/projects/42/merge_requests/55/notes", r.URL.Path)
		var req gitlabgateway.CreateMergeRequestNoteOptions
		require.NoError(t, decodeJSON(r, &req))
		require.NotNil(t, req.Body)
		body = *req.Body
		writeJSON(t, w, gitlabgateway.Note{ID: 90})
	}))
	defer srv.Close()

	repo := newReviewTestRepository(t, srv)
	_, err := repo.SubmitReview(t.Context(), &MR{Number: 55}, forge.SubmitReviewRequest{
		Disposition: forge.ReviewDispositionRequestChanges,
	})
	require.NoError(t, err)
	assert.Equal(t, "Changes requested.", body)
}

func TestRepository_SubmitReview_commentOnlyBody(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v4/projects/42/merge_requests/55/notes", r.URL.Path)
		var req gitlabgateway.CreateMergeRequestNoteOptions
		require.NoError(t, decodeJSON(r, &req))
		require.NotNil(t, req.Body)
		body = *req.Body
		writeJSON(t, w, gitlabgateway.Note{ID: 90})
	}))
	defer srv.Close()

	repo := newReviewTestRepository(t, srv)
	_, err := repo.SubmitReview(t.Context(), &MR{Number: 55}, forge.SubmitReviewRequest{
		Body: "Comment without review disposition.",
	})
	require.NoError(t, err)
	assert.Equal(t, "Comment without review disposition.", body)
}

func TestRepository_SubmitReview_approveWithoutBody(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		assert.Equal(t, "/api/v4/projects/42/merge_requests/55/approve", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	repo := newReviewTestRepository(t, srv)
	_, err := repo.SubmitReview(t.Context(), &MR{Number: 55}, forge.SubmitReviewRequest{
		Disposition: forge.ReviewDispositionApprove,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, requests)
}

func TestRepository_SubmitReview_emptyResultOnError(t *testing.T) {
	var discussions int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects/42/merge_requests/55":
			writeJSON(t, w, gitlabgateway.MergeRequest{
				DiffRefs: gitlabgateway.MergeRequestDiffRefs{
					BaseSHA: "base", HeadSHA: "head", StartSHA: "start",
				},
			})
		case "/api/v4/projects/42/merge_requests/55/discussions":
			discussions++
			if discussions == 2 {
				http.Error(w, "failed", http.StatusInternalServerError)
				return
			}
			writeJSON(t, w, gitlabgateway.Discussion{
				ID:    "first",
				Notes: []*gitlabgateway.DiscussionNote{{ID: 101}},
			})
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	repo := newReviewTestRepository(t, srv)
	result, err := repo.SubmitReview(t.Context(), &MR{Number: 55}, forge.SubmitReviewRequest{
		Comments: []forge.SubmitReviewCommentRequest{
			{Path: "a.go", Range: forge.ReviewThreadLine(1), Body: "First."},
			{Path: "a.go", Range: forge.ReviewThreadLine(2), Body: "Second."},
		},
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "create discussion on a.go:2")
	assert.Equal(t, forge.SubmitReviewResult{}, result)
	assert.Equal(t, 2, discussions)
}

func TestRepository_SubmitReview_emptyResultOnDispositionError(t *testing.T) {
	var discussionCreated bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects/42/merge_requests/55":
			writeJSON(t, w, gitlabgateway.MergeRequest{
				DiffRefs: gitlabgateway.MergeRequestDiffRefs{
					BaseSHA: "base", HeadSHA: "head", StartSHA: "start",
				},
			})
		case "/api/v4/projects/42/merge_requests/55/discussions":
			discussionCreated = true
			writeJSON(t, w, gitlabgateway.Discussion{
				ID:    "first",
				Notes: []*gitlabgateway.DiscussionNote{{ID: 101}},
			})
		case "/api/v4/projects/42/merge_requests/55/notes":
			http.Error(w, "failed", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	repo := newReviewTestRepository(t, srv)
	result, err := repo.SubmitReview(t.Context(), &MR{Number: 55}, forge.SubmitReviewRequest{
		Body: "Summary.",
		Comments: []forge.SubmitReviewCommentRequest{
			{Path: "a.go", Range: forge.ReviewThreadLine(1), Body: "Comment."},
		},
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "post submission body")
	assert.Equal(t, forge.SubmitReviewResult{}, result)
	assert.True(t, discussionCreated)
}

func TestRepository_SubmitReview_panicsForReplyFromAnotherMergeRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(
		http.ResponseWriter,
		*http.Request,
	) {
		t.Fatal("unexpected request")
	}))
	defer srv.Close()

	repo := newReviewTestRepository(t, srv)
	assert.Panics(t, func() {
		_, _ = repo.SubmitReview(t.Context(), &MR{Number: 55}, forge.SubmitReviewRequest{
			Comments: []forge.SubmitReviewCommentRequest{
				{
					ReplyTo: &MRDiscussion{DiscussionID: "thread", MRNumber: 54},
					Body:    "Wrong merge request.",
				},
			},
		})
	})
}

func TestRepository_SubmitReview_panicsForUnknownDisposition(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer srv.Close()

	repo := newReviewTestRepository(t, srv)
	assert.Panics(t, func() {
		_, _ = repo.SubmitReview(t.Context(), &MR{Number: 55}, forge.SubmitReviewRequest{
			Disposition: forge.ReviewDisposition(99),
			Comments: []forge.SubmitReviewCommentRequest{
				{
					Path:  "a.go",
					Range: forge.ReviewThreadLine(1),
					Body:  "Comment.",
				},
			},
		})
	})
	assert.Zero(t, requests)
}

func TestNewDiscussionOptions_leftRange(t *testing.T) {
	options := newDiscussionOptions(forge.SubmitReviewCommentRequest{
		Path:  "old.go",
		Range: forge.ReviewThreadRange{StartLine: 6, EndLine: 8},
		Side:  forge.ReviewThreadSideLeft,
		Body:  "Removed range.",
	}, &gitlabgateway.MergeRequestDiffRefs{
		BaseSHA: "base", HeadSHA: "head", StartSHA: "start",
	})

	require.NotNil(t, options.Position)
	position := options.Position
	require.NotNil(t, position.OldLine)
	assert.Equal(t, int64(8), *position.OldLine)
	assert.Nil(t, position.NewLine)
	require.NotNil(t, position.LineRange)
	assertLinePosition(t, position.LineRange.Start, "old.go", "old", 6, 0)
	assertLinePosition(t, position.LineRange.End, "old.go", "old", 8, 0)
}

func TestNewDiscussionOptions_fileIgnoresSide(t *testing.T) {
	options := newDiscussionOptions(forge.SubmitReviewCommentRequest{
		Path: "file.go",
		Side: forge.ReviewThreadSide(99),
		Body: "Whole file.",
	}, &gitlabgateway.MergeRequestDiffRefs{
		BaseSHA: "base", HeadSHA: "head", StartSHA: "start",
	})

	assertFilePosition(t, options.Position, "file.go")
}

func TestNewDiscussionOptions_panicsForUnknownSide(t *testing.T) {
	assert.Panics(t, func() {
		newDiscussionOptions(forge.SubmitReviewCommentRequest{
			Path:  "a.go",
			Range: forge.ReviewThreadLine(1),
			Side:  forge.ReviewThreadSide(99),
			Body:  "Comment.",
		}, &gitlabgateway.MergeRequestDiffRefs{})
	})
}

func TestRepository_UpdateReviewCommentAndResolveThread(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch requests {
		case 1:
			assert.Equal(t,
				"/api/v4/projects/42/merge_requests/55/discussions/target%2Fthread/notes/88",
				r.URL.EscapedPath())
			writeJSON(t, w, gitlabgateway.DiscussionNote{ID: 88})
		case 2, 3:
			assert.Equal(t,
				"/api/v4/projects/42/merge_requests/55/discussions/target%2Fthread",
				r.URL.EscapedPath())
			var req gitlabgateway.ResolveMergeRequestDiscussionOptions
			require.NoError(t, decodeJSON(r, &req))
			require.NotNil(t, req.Resolved)
			assert.Equal(t, requests == 2, *req.Resolved)
			writeJSON(t, w, gitlabgateway.Discussion{ID: "target/thread"})
		default:
			t.Fatalf("unexpected request %d", requests)
		}
	}))
	defer srv.Close()

	repo := newReviewTestRepository(t, srv)
	require.NoError(t, repo.UpdateReviewComment(
		t.Context(), &MRReviewComment{
			DiscussionID: "target/thread",
			NoteID:       88,
			MRNumber:     55,
		}, "Edited.",
	))
	threadID := &MRDiscussion{DiscussionID: "target/thread", MRNumber: 55}
	require.NoError(t, repo.ResolveReviewThread(t.Context(), threadID))
	require.NoError(t, repo.UnresolveReviewThread(t.Context(), threadID))
	assert.Equal(t, 3, requests)
}

func TestRepository_UpdateReviewComment_panicsForChangeCommentID(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer srv.Close()

	repo := newReviewTestRepository(t, srv)
	assert.Panics(t, func() {
		_ = repo.UpdateReviewComment(
			t.Context(), &MRComment{Number: 88, MRNumber: 55}, "Edited.",
		)
	})
	assert.Zero(t, requests)
}

func TestRepository_ResolveReviewThread_panicsForReviewCommentID(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer srv.Close()

	repo := newReviewTestRepository(t, srv)
	assert.Panics(t, func() {
		_ = repo.ResolveReviewThread(t.Context(), &MRReviewComment{
			DiscussionID: "target/thread",
			NoteID:       88,
			MRNumber:     55,
		})
	})
	assert.Zero(t, requests)
}

func TestRepository_SubmitReview_contextRangeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects/42/merge_requests/55":
			writeJSON(t, w, gitlabgateway.MergeRequest{DiffRefs: gitlabgateway.MergeRequestDiffRefs{
				BaseSHA: "base", HeadSHA: "head", StartSHA: "start",
			}})
		case "/api/v4/projects/42/merge_requests/55/discussions":
			http.Error(w, "position is incomplete", http.StatusBadRequest)
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	repo := newReviewTestRepository(t, srv)
	_, err := repo.SubmitReview(t.Context(), &MR{Number: 55}, forge.SubmitReviewRequest{
		Comments: []forge.SubmitReviewCommentRequest{{
			Path: "a.go", Range: forge.ReviewThreadRange{StartLine: 2, EndLine: 3},
			Body: "Context range.",
		}},
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "GitLab context ranges require both old and new coordinates")
}

func newReviewTestRepository(t *testing.T, srv *httptest.Server) *Repository {
	t.Helper()
	client, err := gitlabgateway.NewClient(
		gitlabgateway.StaticTokenSource(gitlabgateway.Token{
			Type:  gitlabgateway.TokenTypePrivateToken,
			Value: "token",
		}),
		&gitlabgateway.ClientOptions{
			BaseURL:    srv.URL,
			HTTPClient: srv.Client(),
		},
	)
	require.NoError(t, err)
	return &Repository{
		client: client,
		repoID: 42,
		log:    silogtest.New(t),
	}
}

func decodeJSON(r *http.Request, dst any) error {
	return json.UnmarshalRead(r.Body, dst)
}

func assertRightRangePosition(
	t *testing.T,
	position *gitlabgateway.PositionOptions,
	path string,
	start, end int64,
) {
	t.Helper()
	require.NotNil(t, position)
	require.NotNil(t, position.PositionType)
	assert.Equal(t, "text", *position.PositionType)
	assert.Equal(t, path, *position.OldPath)
	assert.Equal(t, path, *position.NewPath)
	assert.Nil(t, position.OldLine)
	require.NotNil(t, position.NewLine)
	assert.Equal(t, end, *position.NewLine)
	require.NotNil(t, position.LineRange)
	assertLinePosition(t, position.LineRange.Start, path, "new", 0, start)
	assertLinePosition(t, position.LineRange.End, path, "new", 0, end)
}

func assertLeftLinePosition(
	t *testing.T,
	position *gitlabgateway.PositionOptions,
	path string,
	line int64,
) {
	t.Helper()
	require.NotNil(t, position)
	require.NotNil(t, position.PositionType)
	assert.Equal(t, "text", *position.PositionType)
	assert.Equal(t, path, *position.OldPath)
	assert.Equal(t, path, *position.NewPath)
	require.NotNil(t, position.OldLine)
	assert.Equal(t, line, *position.OldLine)
	assert.Nil(t, position.NewLine)
	assert.Nil(t, position.LineRange)
}

func assertFilePosition(
	t *testing.T,
	position *gitlabgateway.PositionOptions,
	path string,
) {
	t.Helper()
	require.NotNil(t, position)
	require.NotNil(t, position.PositionType)
	assert.Equal(t, "file", *position.PositionType)
	assert.Equal(t, path, *position.OldPath)
	assert.Equal(t, path, *position.NewPath)
	assert.Nil(t, position.OldLine)
	assert.Nil(t, position.NewLine)
	assert.Nil(t, position.LineRange)
}

func assertLinePosition(
	t *testing.T,
	position *gitlabgateway.LinePositionOptions,
	path string,
	wantType string,
	wantOld, wantNew int64,
) {
	t.Helper()
	require.NotNil(t, position)
	require.NotNil(t, position.Type)
	assert.Equal(t, wantType, *position.Type)
	require.NotNil(t, position.LineCode)
	assert.Equal(t, fmt.Sprintf(
		"%x_%d_%d",
		sha1.Sum([]byte(path)),
		wantOld,
		wantNew,
	), *position.LineCode)
	if wantOld == 0 {
		assert.Nil(t, position.OldLine)
	} else {
		require.NotNil(t, position.OldLine)
		assert.Equal(t, wantOld, *position.OldLine)
	}
	if wantNew == 0 {
		assert.Nil(t, position.NewLine)
	} else {
		require.NotNil(t, position.NewLine)
		assert.Equal(t, wantNew, *position.NewLine)
	}
}
