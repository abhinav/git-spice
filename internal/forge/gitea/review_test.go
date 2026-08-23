package gitea

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/sliceutil"
)

func TestRepository_SubmitReview(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/captain/warp-core/pulls/42/reviews":
			assert.Equal(t, http.MethodPost, r.Method)
			assertJSONBody(t, r, `{
				"event":"REQUEST_CHANGES",
				"body":"Rework the plasma flow.",
				"comments":[
					{"path":"engine.go","body":"New-side note.","new_position":12},
					{"path":"legacy.go","body":"Old-side note.","old_position":7},
					{"path":"engine.go","body":"Reply note.","new_position":12}
				]
			}`)
			writeJSON(t, w, http.StatusOK, giteagw.PullReview{ID: 100})
		case "/api/v1/repos/captain/warp-core/pulls/42/reviews/100/comments":
			writeJSON(t, w, http.StatusOK, []*giteagw.PullReviewComment{
				{ID: 1002, Body: "Reply note.", Path: "engine.go", Position: 12},
				{ID: 1001, Body: "Old-side note.", Path: "legacy.go", OriginalPosition: 7},
				{ID: 1000, Body: "New-side note.", Path: "engine.go", Position: 12},
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	result, err := repo.SubmitReview(
		t.Context(),
		&PR{Number: 42},
		forge.SubmitReviewRequest{
			Body:        "Rework the plasma flow.",
			Disposition: forge.ReviewDispositionRequestChanges,
			Comments: []forge.SubmitReviewCommentRequest{
				{
					Path:  "engine.go",
					Range: forge.ReviewThreadLine(12),
					Body:  "New-side note.",
					Side:  forge.ReviewThreadSideRight,
				},
				{
					Path:  "legacy.go",
					Range: forge.ReviewThreadLine(7),
					Body:  "Old-side note.",
					Side:  forge.ReviewThreadSideLeft,
				},
				{
					ReplyTo: &reviewThreadID{prNumber: 42, path: "engine.go", line: 12},
					Path:    "ignored.go",
					Range:   forge.ReviewThreadLine(99),
					Body:    "Reply note.",
					Side:    forge.ReviewThreadSideLeft,
				},
			},
		},
	)
	require.NoError(t, err)
	require.Len(t, result.Comments, 3)
	assert.Equal(t, &reviewThreadID{prNumber: 42, path: "engine.go", line: 12}, result.Comments[0].ThreadID)
	assert.Equal(t, reviewCommentID(1000), result.Comments[0].CommentID)
	assert.Equal(t, &reviewThreadID{prNumber: 42, path: "legacy.go", line: -7}, result.Comments[1].ThreadID)
	assert.Equal(t, reviewCommentID(1001), result.Comments[1].CommentID)
	assert.Equal(t, result.Comments[0].ThreadID, result.Comments[2].ThreadID)
	assert.Equal(t, reviewCommentID(1002), result.Comments[2].CommentID)
}

func TestRepository_SubmitReview_errorResultIsEmpty(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/captain/warp-core/pulls/42/reviews":
			writeJSON(t, w, http.StatusOK, giteagw.PullReview{ID: 100})
		case "/api/v1/repos/captain/warp-core/pulls/42/reviews/100/comments":
			writeJSON(t, w, http.StatusOK, []*giteagw.PullReviewComment{
				{ID: 1000, Body: "First.", Path: "engine.go", Position: 12},
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	result, err := repo.SubmitReview(
		t.Context(),
		&PR{Number: 42},
		forge.SubmitReviewRequest{
			Comments: []forge.SubmitReviewCommentRequest{
				{Path: "engine.go", Range: forge.ReviewThreadLine(12), Body: "First."},
				{Path: "engine.go", Range: forge.ReviewThreadLine(13), Body: "Second."},
			},
		},
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "identify submitted review comment 2")
	assert.Empty(t, result.Comments)
}

func TestRepository_SubmitReview_usesStartLineForMultiLineRange(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/captain/warp-core/pulls/42/reviews":
			assertJSONBody(t, r, `{
				"event":"COMMENT",
				"comments":[{
					"path":"engine.go",
					"body":"This whole block needs reworking.",
					"new_position":12
				}]
			}`)
			writeJSON(t, w, http.StatusOK, giteagw.PullReview{ID: 100})
		case "/api/v1/repos/captain/warp-core/pulls/42/reviews/100/comments":
			writeJSON(t, w, http.StatusOK, []*giteagw.PullReviewComment{
				{
					ID:       1000,
					Path:     "engine.go",
					Body:     "This whole block needs reworking.",
					Position: 12,
				},
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	result, err := repo.SubmitReview(
		t.Context(),
		&PR{Number: 42},
		forge.SubmitReviewRequest{
			Comments: []forge.SubmitReviewCommentRequest{
				{
					Path:  "engine.go",
					Range: forge.ReviewThreadRange{StartLine: 12, EndLine: 14},
					Body:  "This whole block needs reworking.",
				},
			},
		},
	)
	require.NoError(t, err)
	require.Len(t, result.Comments, 1)
	assert.Equal(
		t,
		&reviewThreadID{prNumber: 42, path: "engine.go", line: 12},
		result.Comments[0].ThreadID,
	)
}

func TestRepository_SubmitReview_panicsForInvalidInternalValue(t *testing.T) {
	tests := []struct {
		name    string
		request forge.SubmitReviewRequest
	}{
		{
			name: "UnsupportedDisposition",
			request: forge.SubmitReviewRequest{
				Disposition: forge.ReviewDisposition(99),
			},
		},
		{
			name: "UnsupportedSide",
			request: forge.SubmitReviewRequest{
				Comments: []forge.SubmitReviewCommentRequest{
					{
						Path:  "engine.go",
						Range: forge.ReviewThreadLine(12),
						Body:  "Note.",
						Side:  forge.ReviewThreadSide(99),
					},
				},
			},
		},
		{
			name: "ForeignThreadID",
			request: forge.SubmitReviewRequest{
				Comments: []forge.SubmitReviewCommentRequest{
					{ReplyTo: &PRComment{ID: 88}, Body: "Reply."},
				},
			},
		},
		{
			name: "CrossPullRequestThread",
			request: forge.SubmitReviewRequest{
				Comments: []forge.SubmitReviewCommentRequest{
					{
						ReplyTo: &reviewThreadID{
							prNumber: 41,
							path:     "engine.go",
							line:     12,
						},
						Body: "Reply.",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t, func(http.ResponseWriter, *http.Request) {
				t.Fatal("invalid internal review value reached Gitea")
			})
			defer srv.Close()

			repo := newTestRepo(t, srv)
			assert.Panics(t, func() {
				_, _ = repo.SubmitReview(
					t.Context(),
					&PR{Number: 42},
					tt.request,
				)
			})
		})
	}
}

func TestRepository_SubmitReview_requiresProviderBody(t *testing.T) {
	srv := newTestServer(t, func(http.ResponseWriter, *http.Request) {
		t.Fatal("invalid provider review reached Gitea")
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	_, err := repo.SubmitReview(
		t.Context(),
		&PR{Number: 42},
		forge.SubmitReviewRequest{Body: " \t"},
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "COMMENT requires a body or comment")
}

func TestRepository_SubmitReview_addsRequestChangesBody(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/repos/captain/warp-core/pulls/42/reviews", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		assertJSONBody(t, r, `{
			"event":"REQUEST_CHANGES",
			"body":"Requesting changes"
		}`)
		writeJSON(t, w, http.StatusOK, giteagw.PullReview{ID: 100})
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	result, err := repo.SubmitReview(
		t.Context(),
		&PR{Number: 42},
		forge.SubmitReviewRequest{
			Disposition: forge.ReviewDispositionRequestChanges,
		},
	)
	require.NoError(t, err)
	assert.Empty(t, result.Comments)
}

func TestRepository_ListReviewThreads_skipsNonSubmittedReviews(t *testing.T) {
	var commentListCalls int
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/captain/warp-core/pulls/42/reviews":
			writeJSON(t, w, http.StatusOK, []*giteagw.PullReview{
				{
					ID:                100,
					State:             giteagw.ReviewStatePending,
					CodeCommentsCount: 1,
				},
				{
					ID:                101,
					State:             giteagw.ReviewStateRequestReview,
					CodeCommentsCount: 1,
				},
				{
					ID:                102,
					State:             giteagw.ReviewState("FUTURE_STATE"),
					CodeCommentsCount: 1,
				},
			})
		case "/api/v1/repos/captain/warp-core/pulls/42/reviews/100/comments",
			"/api/v1/repos/captain/warp-core/pulls/42/reviews/101/comments",
			"/api/v1/repos/captain/warp-core/pulls/42/reviews/102/comments":
			commentListCalls++
			writeJSON(t, w, http.StatusOK, []*giteagw.PullReviewComment{
				{ID: 1000, Body: "Unpublished draft.", Path: "engine.go", Position: 12},
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	threads, err := sliceutil.CollectErr(
		repo.ListReviewThreads(t.Context(), &PR{Number: 42}),
	)
	require.NoError(t, err)
	assert.Empty(t, threads)
	assert.Zero(t, commentListCalls)
}

func TestRepository_ListReviewThreads(t *testing.T) {
	created := time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC)
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/captain/warp-core/pulls/42/reviews":
			writeJSON(t, w, http.StatusOK, []*giteagw.PullReview{
				{ID: 101, State: giteagw.ReviewStateComment, CodeCommentsCount: 1},
				{ID: 100, State: giteagw.ReviewStateComment, CodeCommentsCount: 2},
				{ID: 102},
			})
		case "/api/v1/repos/captain/warp-core/pulls/42/reviews/100/comments":
			writeJSON(t, w, http.StatusOK, []*giteagw.PullReviewComment{
				{
					ID:        1000,
					Body:      "Root note.",
					User:      &giteagw.User{Login: "spock"},
					CreatedAt: created,
					Path:      "engine.go",
					Position:  12,
				},
				{
					ID:               1001,
					Body:             "Old-side note.",
					User:             &giteagw.User{Login: "uhura"},
					Path:             "legacy.go",
					OriginalPosition: 7,
					Resolver:         &giteagw.User{Login: "kirk"},
				},
			})
		case "/api/v1/repos/captain/warp-core/pulls/42/reviews/101/comments":
			writeJSON(t, w, http.StatusOK, []*giteagw.PullReviewComment{
				{
					ID:        1002,
					Body:      "Reply note.",
					User:      &giteagw.User{Login: "scotty"},
					CreatedAt: created.Add(time.Minute),
					Path:      "engine.go",
					Position:  12,
				},
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	threads, err := sliceutil.CollectErr(
		repo.ListReviewThreads(t.Context(), &PR{Number: 42}),
	)
	require.NoError(t, err)
	require.Len(t, threads, 2)

	assert.Equal(t, &reviewThreadID{prNumber: 42, path: "engine.go", line: 12}, threads[0].ID)
	assert.Equal(t, "engine.go", threads[0].Path)
	assert.Equal(t, forge.ReviewThreadLine(12), threads[0].Range)
	assert.Equal(t, forge.ReviewThreadSideRight, threads[0].Side)
	require.NotNil(t, threads[0].Resolved)
	assert.False(t, *threads[0].Resolved)
	require.Len(t, threads[0].Comments, 2)
	assert.Equal(t, reviewCommentID(1000), threads[0].Comments[0].ID)
	assert.Equal(t, "spock", threads[0].Comments[0].Author)
	assert.Equal(t, created, threads[0].Comments[0].CreatedAt)
	assert.Equal(t, reviewCommentID(1002), threads[0].Comments[1].ID)

	assert.Equal(t, &reviewThreadID{prNumber: 42, path: "legacy.go", line: -7}, threads[1].ID)
	assert.Equal(t, forge.ReviewThreadLine(7), threads[1].Range)
	assert.Equal(t, forge.ReviewThreadSideLeft, threads[1].Side)
	require.NotNil(t, threads[1].Resolved)
	assert.True(t, *threads[1].Resolved)
	assert.Nil(t, threads[1].Outdated)
}

func TestRepository_ListReviewerStates(t *testing.T) {
	first := time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC)
	latest := first.Add(time.Hour)
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/repos/captain/warp-core/pulls/42/reviews", r.URL.Path)
		switch r.URL.Query().Get("page") {
		case "":
			w.Header().Set("X-Next-Page", "2")
			writeJSON(t, w, http.StatusOK, []*giteagw.PullReview{
				{
					ID:          100,
					Reviewer:    &giteagw.User{Login: "spock"},
					State:       giteagw.ReviewStateApproved,
					CommitID:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					SubmittedAt: first,
				},
				{
					ID:          101,
					Reviewer:    &giteagw.User{Login: "uhura"},
					State:       giteagw.ReviewStateRequestChanges,
					CommitID:    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					SubmittedAt: first,
				},
			})
		case "2":
			writeJSON(t, w, http.StatusOK, []*giteagw.PullReview{
				{
					ID:          102,
					Reviewer:    &giteagw.User{Login: "spock"},
					State:       giteagw.ReviewStateComment,
					CommitID:    "cccccccccccccccccccccccccccccccccccccccc",
					SubmittedAt: latest,
				},
				{
					ID:          105,
					Reviewer:    &giteagw.User{Login: "sulu"},
					State:       giteagw.ReviewStateComment,
					CommitID:    "dddddddddddddddddddddddddddddddddddddddd",
					SubmittedAt: latest,
				},
				{ID: 103, Reviewer: &giteagw.User{Login: "kirk"}, State: giteagw.ReviewStatePending},
				{ID: 104, Reviewer: &giteagw.User{Login: "chapel"}, State: giteagw.ReviewStateApproved, Dismissed: true},
			})
		default:
			t.Fatalf("unexpected page %q", r.URL.Query().Get("page"))
		}
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	states, err := sliceutil.CollectErr(
		repo.ListReviewerStates(t.Context(), &PR{Number: 42}),
	)
	require.NoError(t, err)
	require.Len(t, states, 2)

	byReviewer := make(map[string]*forge.ReviewerState)
	for _, state := range states {
		byReviewer[state.Reviewer] = state
	}
	assert.Equal(t, forge.ReviewDispositionApprove, byReviewer["spock"].Disposition)
	assert.Equal(t, git.Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), byReviewer["spock"].CommitHash)
	assert.Equal(t, first, byReviewer["spock"].SubmittedAt)
	assert.Equal(t, forge.ReviewDispositionRequestChanges, byReviewer["uhura"].Disposition)
	assert.NotContains(t, byReviewer, "sulu")
	assert.NotContains(t, byReviewer, "kirk")
	assert.NotContains(t, byReviewer, "chapel")
}

func TestRepository_ListReviewerStates_omitsCommentSubmission(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/repos/captain/warp-core/pulls/42/reviews", r.URL.Path)
		writeJSON(t, w, http.StatusOK, []*giteagw.PullReview{
			{
				ID:       100,
				Reviewer: &giteagw.User{Login: "spock"},
				State:    giteagw.ReviewStateComment,
			},
		})
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	states, err := sliceutil.CollectErr(
		repo.ListReviewerStates(t.Context(), &PR{Number: 42}),
	)
	require.NoError(t, err)
	assert.Empty(t, states)
}

func TestRepository_UpdateReviewComment(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/repos/captain/warp-core/issues/comments/88", r.URL.Path)
		assert.Equal(t, http.MethodPatch, r.Method)
		assertJSONBody(t, r, `{"body":"Updated review note."}`)
		writeJSON(t, w, http.StatusOK, giteagw.Comment{ID: 88})
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	require.NoError(t, repo.UpdateReviewComment(
		t.Context(), reviewCommentID(88), "Updated review note.",
	))
}

func TestRepository_UpdateReviewComment_notFound(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/repos/captain/warp-core/issues/comments/88", r.URL.Path)
		http.NotFound(w, r)
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	err := repo.UpdateReviewComment(t.Context(), reviewCommentID(88), "Updated.")
	require.Error(t, err)
	assert.True(t, errors.Is(err, forge.ErrNotFound))
}

func TestRepository_UpdateReviewComment_rejectsWrongIDType(t *testing.T) {
	var calls int
	srv := newTestServer(t, func(http.ResponseWriter, *http.Request) {
		calls++
	})
	defer srv.Close()

	repo := newTestRepo(t, srv)
	assert.Panics(t, func() {
		_ = repo.UpdateReviewComment(t.Context(), &PRComment{ID: 88}, "Updated.")
	})
	assert.Zero(t, calls)
}

func TestReviewThreadID_String(t *testing.T) {
	id := &reviewThreadID{prNumber: 42, path: "dir:part/engine.go", line: -12}
	assert.Equal(t, "42:dir:part/engine.go:-12", id.String())
}

func TestReviewCommentID_String(t *testing.T) {
	assert.Equal(t, "88", reviewCommentID(88).String())
}

func TestMustReviewDispositionToGitea(t *testing.T) {
	tests := []struct {
		give forge.ReviewDisposition
		want giteagw.ReviewState
	}{
		{give: forge.ReviewDispositionNone, want: giteagw.ReviewStateComment},
		{give: forge.ReviewDispositionApprove, want: giteagw.ReviewStateApproved},
		{give: forge.ReviewDispositionRequestChanges, want: giteagw.ReviewStateRequestChanges},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.give), func(t *testing.T) {
			assert.Equal(t, tt.want, mustReviewDispositionToGitea(tt.give))
		})
	}
}
