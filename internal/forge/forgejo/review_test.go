package forgejo

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/forgejo"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/sliceutil"
)

func TestRepository_SubmitReview(t *testing.T) {
	var reviewCreates int
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/repos/owner/repo":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.Repository{
					FullName:    "owner/repo",
					Permissions: &forgejo.Permission{Push: true},
				})
			case "/api/v1/user":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.User{ID: 1})
			case "/api/v1/repos/owner/repo/pulls/42/reviews":
				reviewCreates++
				switch reviewCreates {
				case 1:
					assertGatewayJSONBody(t, r, `{
						"event":"COMMENT",
						"comments":[{
							"body":"Root comment.",
							"path":"review.go",
							"new_position":3
						}]
					}`)
					writeGatewayJSON(t, w, http.StatusOK, forgejo.PullReview{ID: 70})
				case 2:
					assertGatewayJSONBody(t, r, `{
						"body":"Review body.",
						"event":"REQUEST_CHANGES",
						"comments":[{
							"body":"Left-side comment.",
							"path":"review.go",
							"old_position":4
						}]
					}`)
					writeGatewayJSON(t, w, http.StatusOK, forgejo.PullReview{ID: 71})
				default:
					t.Fatalf("unexpected review creation %d", reviewCreates)
				}
			case "/api/v1/repos/owner/repo/pulls/42/reviews/70/comments":
				switch r.Method {
				case http.MethodGet:
					writeGatewayJSON(t, w, http.StatusOK, []*forgejo.PullReviewComment{
						{
							ID:       101,
							ReviewID: 70,
							Body:     "Root comment.",
							Path:     "review.go",
							Position: 3,
						},
					})
				case http.MethodPost:
					assertGatewayJSONBody(t, r, `{
						"body":"Reply comment.",
						"path":"review.go",
						"new_position":3
					}`)
					writeGatewayJSON(t, w, http.StatusOK, forgejo.PullReviewComment{
						ID:       103,
						ReviewID: 70,
						Body:     "Reply comment.",
						Path:     "review.go",
						Position: 3,
					})
				default:
					t.Fatalf("unexpected method: %s", r.Method)
				}
			case "/api/v1/repos/owner/repo/pulls/42/reviews/71/comments":
				writeGatewayJSON(t, w, http.StatusOK, []*forgejo.PullReviewComment{
					{
						ID:               102,
						ReviewID:         71,
						Body:             "Left-side comment.",
						Path:             "review.go",
						OriginalPosition: 4,
					},
				})
			default:
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
		}))
	defer srv.Close()

	repo := newTestRepository(t, srv)
	rootReview, err := repo.SubmitReview(
		t.Context(),
		&PR{Number: 42},
		forge.SubmitReviewRequest{
			Comments: []forge.SubmitReviewCommentRequest{
				{
					Path:  "review.go",
					Range: forge.ReviewThreadLine(3),
					Side:  forge.ReviewThreadSideRight,
					Body:  "Root comment.",
				},
			},
		},
	)
	require.NoError(t, err)
	require.Len(t, rootReview.Comments, 1)
	root := rootReview.Comments[0]
	require.NotNil(t, root.ThreadID)
	require.NotNil(t, root.CommentID)
	assert.Equal(t, "42:70:review.go:3", root.ThreadID.String())
	assert.Equal(t, "101", root.CommentID.String())

	mixedReview, err := repo.SubmitReview(
		t.Context(),
		&PR{Number: 42},
		forge.SubmitReviewRequest{
			Body:        "Review body.",
			Disposition: forge.ReviewDispositionRequestChanges,
			Comments: []forge.SubmitReviewCommentRequest{
				{
					Path:  "review.go",
					Range: forge.ReviewThreadLine(4),
					Side:  forge.ReviewThreadSideLeft,
					Body:  "Left-side comment.",
				},
				{
					ReplyTo: root.ThreadID,
					Body:    "Reply comment.",
				},
			},
		},
	)
	require.NoError(t, err)
	require.Len(t, mixedReview.Comments, 2)
	assert.NotEqual(t, root.ThreadID, mixedReview.Comments[0].ThreadID)
	assert.Equal(t, "102", mixedReview.Comments[0].CommentID.String())
	assert.Equal(t, root.ThreadID, mixedReview.Comments[1].ThreadID)
	assert.Equal(t, "103", mixedReview.Comments[1].CommentID.String())
}

func TestRepository_SubmitReviewUsesStartLineForMultiLineRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/repos/owner/repo":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.Repository{
					FullName: "owner/repo",
				})
			case "/api/v1/user":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.User{ID: 1})
			case "/api/v1/repos/owner/repo/pulls/42/reviews":
				assertGatewayJSONBody(t, r, `{
					"event":"COMMENT",
					"comments":[{
						"body":"Range comment.",
						"path":"review.go",
						"new_position":3
					}]
				}`)
				writeGatewayJSON(t, w, http.StatusOK, forgejo.PullReview{ID: 70})
			case "/api/v1/repos/owner/repo/pulls/42/reviews/70/comments":
				writeGatewayJSON(t, w, http.StatusOK, []*forgejo.PullReviewComment{
					{
						ID:       101,
						ReviewID: 70,
						Body:     "Range comment.",
						Path:     "review.go",
						Position: 3,
					},
				})
			default:
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
		}))
	t.Cleanup(srv.Close)
	repo := newTestRepository(t, srv)

	result, err := repo.SubmitReview(
		t.Context(),
		&PR{Number: 42},
		forge.SubmitReviewRequest{
			Comments: []forge.SubmitReviewCommentRequest{
				{
					Path:  "review.go",
					Range: forge.ReviewThreadRange{StartLine: 3, EndLine: 4},
					Side:  forge.ReviewThreadSideRight,
					Body:  "Range comment.",
				},
			},
		},
	)
	require.NoError(t, err)
	require.Len(t, result.Comments, 1)
	assert.Equal(t, "42:70:review.go:3", result.Comments[0].ThreadID.String())
}

func TestRepository_SubmitReviewAddsRequestChangesPlaceholder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/repos/owner/repo":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.Repository{
					FullName: "owner/repo",
				})
			case "/api/v1/user":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.User{ID: 1})
			case "/api/v1/repos/owner/repo/pulls/42/reviews":
				assertGatewayJSONBody(t, r, `{
					"body":"Requesting changes",
					"event":"REQUEST_CHANGES"
				}`)
				writeGatewayJSON(t, w, http.StatusOK, forgejo.PullReview{ID: 70})
			default:
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
		}))
	t.Cleanup(srv.Close)
	repo := newTestRepository(t, srv)

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

func TestRepository_SubmitReviewReturnsEmptyResultOnError(t *testing.T) {
	var replies int
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/repos/owner/repo":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.Repository{
					FullName: "owner/repo",
				})
			case "/api/v1/user":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.User{ID: 1})
			case "/api/v1/repos/owner/repo/pulls/42/reviews/70/comments":
				replies++
				if replies == 2 {
					http.Error(w, "unavailable", http.StatusServiceUnavailable)
					return
				}
				writeGatewayJSON(t, w, http.StatusOK, forgejo.PullReviewComment{
					ID:       101,
					ReviewID: 70,
					Body:     "First reply.",
					Path:     "review.go",
					Position: 3,
				})
			default:
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
		}))
	t.Cleanup(srv.Close)
	repo := newTestRepository(t, srv)
	threadID := &reviewThreadID{
		prNumber: 42,
		reviewID: 70,
		path:     "review.go",
		position: 3,
	}

	result, err := repo.SubmitReview(
		t.Context(),
		&PR{Number: 42},
		forge.SubmitReviewRequest{
			Comments: []forge.SubmitReviewCommentRequest{
				{ReplyTo: threadID, Body: "First reply."},
				{ReplyTo: threadID, Body: "Second reply."},
			},
		},
	)
	require.ErrorContains(t, err, "create review reply")
	assert.Empty(t, result.Comments)
}

func TestRepository_ListReviewThreadsSkipsPendingReviews(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/repos/owner/repo":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.Repository{
					FullName: "owner/repo",
				})
			case "/api/v1/user":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.User{ID: 1})
			case "/api/v1/repos/owner/repo/pulls/42/reviews":
				writeGatewayJSON(t, w, http.StatusOK, []*forgejo.PullReview{
					{ID: 70, State: forgejo.PullReviewStatePending},
					{ID: 71, State: forgejo.PullReviewStateComment},
				})
			case "/api/v1/repos/owner/repo/pulls/42/reviews/71/comments":
				writeGatewayJSON(t, w, http.StatusOK, []*forgejo.PullReviewComment{
					{
						ID:       101,
						ReviewID: 71,
						Body:     "Submitted comment.",
						Path:     "review.go",
						Position: 3,
					},
				})
			default:
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
		}))
	t.Cleanup(srv.Close)

	repo := newTestRepository(t, srv)
	threads, err := sliceutil.CollectErr(
		repo.ListReviewThreads(t.Context(), &PR{Number: 42}),
	)
	require.NoError(t, err)
	require.Len(t, threads, 1)
	assert.Equal(t, "Submitted comment.", threads[0].Comments[0].Body)
}

func TestRepository_ListReviewThreadsStopsPaging(t *testing.T) {
	var reviewPages int
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/repos/owner/repo":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.Repository{
					FullName: "owner/repo",
				})
			case "/api/v1/user":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.User{ID: 1})
			case "/api/v1/repos/owner/repo/pulls/42/reviews":
				reviewPages++
				if r.URL.Query().Get("page") != "1" {
					t.Fatalf("unexpected review page: %s", r.URL.RawQuery)
				}
				w.Header().Set(
					"Link",
					`<https://forgejo.example/reviews?page=2>; rel="next"`,
				)
				writeGatewayJSON(t, w, http.StatusOK, []*forgejo.PullReview{
					{ID: 70, State: forgejo.PullReviewStateComment},
				})
			case "/api/v1/repos/owner/repo/pulls/42/reviews/70/comments":
				writeGatewayJSON(t, w, http.StatusOK, []*forgejo.PullReviewComment{
					{
						ID:       101,
						ReviewID: 70,
						Body:     "First thread.",
						Path:     "review.go",
						Position: 3,
					},
				})
			default:
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
		}))
	t.Cleanup(srv.Close)
	repo := newTestRepository(t, srv)

	var first *forge.ReviewThread
	for thread, err := range repo.ListReviewThreads(
		t.Context(), &PR{Number: 42},
	) {
		require.NoError(t, err)
		first = thread
		break
	}
	require.NotNil(t, first)
	assert.Equal(t, "First thread.", first.Comments[0].Body)
	assert.Equal(t, 1, reviewPages)
}

func TestRepository_ListReviewerStatesPaginates(t *testing.T) {
	submittedAt := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/repos/owner/repo":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.Repository{
					FullName: "owner/repo",
				})
			case "/api/v1/user":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.User{ID: 1})
			case "/api/v1/repos/owner/repo/pulls/42/reviews":
				switch r.URL.Query().Get("page") {
				case "1":
					w.Header().Set(
						"Link",
						`<https://forgejo.example/reviews?page=2>; rel="next"`,
					)
					writeGatewayJSON(t, w, http.StatusOK, []*forgejo.PullReview{
						{
							ID:          70,
							State:       forgejo.PullReviewStateComment,
							SubmittedAt: submittedAt,
							User:        &forgejo.User{Login: "alice"},
						},
					})
				case "2":
					writeGatewayJSON(t, w, http.StatusOK, []*forgejo.PullReview{
						{
							ID:          71,
							State:       forgejo.PullReviewStateApproved,
							SubmittedAt: submittedAt.Add(time.Minute),
							User:        &forgejo.User{Login: "alice"},
						},
						{
							ID:          72,
							State:       forgejo.PullReviewStateRequestChanges,
							SubmittedAt: submittedAt.Add(2 * time.Minute),
							User:        &forgejo.User{Login: "bob"},
						},
					})
				default:
					t.Fatalf("unexpected review page: %s", r.URL.RawQuery)
				}
			default:
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
		}))
	t.Cleanup(srv.Close)
	repo := newTestRepository(t, srv)

	states, err := sliceutil.CollectErr(
		repo.ListReviewerStates(t.Context(), &PR{Number: 42}),
	)
	require.NoError(t, err)
	require.Len(t, states, 2)
	assert.Equal(t, "alice", states[0].Reviewer)
	assert.Equal(t, forge.ReviewDispositionApprove, states[0].Disposition)
	assert.Equal(t, "bob", states[1].Reviewer)
	assert.Equal(t, forge.ReviewDispositionRequestChanges, states[1].Disposition)
}

func TestRepository_ListReviewerStates_omitsCommentSubmissions(t *testing.T) {
	submittedAt := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/repos/owner/repo":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.Repository{
					FullName: "owner/repo",
				})
			case "/api/v1/user":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.User{ID: 1})
			case "/api/v1/repos/owner/repo/pulls/42/reviews":
				writeGatewayJSON(t, w, http.StatusOK, []*forgejo.PullReview{
					{
						ID:          70,
						State:       forgejo.PullReviewStateComment,
						SubmittedAt: submittedAt,
						User:        &forgejo.User{Login: "bob"},
					},
					{
						ID:          71,
						State:       forgejo.PullReviewStateApproved,
						SubmittedAt: submittedAt,
						User:        &forgejo.User{Login: "alice"},
					},
					{
						ID:          72,
						State:       forgejo.PullReviewStateComment,
						SubmittedAt: submittedAt.Add(time.Minute),
						User:        &forgejo.User{Login: "alice"},
					},
				})
			default:
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
		}))
	t.Cleanup(srv.Close)
	repo := newTestRepository(t, srv)

	states, err := sliceutil.CollectErr(
		repo.ListReviewerStates(t.Context(), &PR{Number: 42}),
	)
	require.NoError(t, err)
	require.Len(t, states, 1)
	assert.Equal(t, "alice", states[0].Reviewer)
	assert.Equal(t, forge.ReviewDispositionApprove, states[0].Disposition)
}

func TestRepository_ListReviews(t *testing.T) {
	submittedAt := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	updatedAt := submittedAt.Add(time.Minute)
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/repos/owner/repo":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.Repository{
					FullName:    "owner/repo",
					Permissions: &forgejo.Permission{Push: true},
				})
			case "/api/v1/user":
				writeGatewayJSON(t, w, http.StatusOK, forgejo.User{ID: 1})
			case "/api/v1/repos/owner/repo/pulls/42/reviews":
				writeGatewayJSON(t, w, http.StatusOK, []*forgejo.PullReview{
					{
						ID:          70,
						State:       forgejo.PullReviewStateComment,
						CommitID:    "1111111111111111111111111111111111111111",
						Stale:       true,
						SubmittedAt: submittedAt,
						User:        &forgejo.User{Login: "alice"},
					},
					{
						ID:          71,
						State:       forgejo.PullReviewStateApproved,
						CommitID:    "2222222222222222222222222222222222222222",
						SubmittedAt: updatedAt,
						User:        &forgejo.User{Login: "alice"},
					},
					{
						ID:    72,
						State: forgejo.PullReviewStateRequestReview,
						User:  &forgejo.User{Login: "bob"},
					},
					{
						ID:        73,
						State:     forgejo.PullReviewStateRequestChanges,
						Dismissed: true,
						User:      &forgejo.User{Login: "carol"},
					},
				})
			case "/api/v1/repos/owner/repo/pulls/42/reviews/70/comments":
				writeGatewayJSON(t, w, http.StatusOK, []*forgejo.PullReviewComment{
					{
						ID:        101,
						ReviewID:  70,
						Body:      "Root comment.",
						Path:      "review.go",
						Position:  3,
						CreatedAt: submittedAt,
						User:      &forgejo.User{Login: "alice"},
					},
					{
						ID:        103,
						ReviewID:  70,
						Body:      "Reply comment.",
						Path:      "review.go",
						Position:  3,
						CreatedAt: updatedAt,
						User:      &forgejo.User{Login: "bob"},
						Resolver:  &forgejo.User{Login: "maintainer"},
					},
					{
						ID:               102,
						ReviewID:         70,
						Body:             "Left comment.",
						Path:             "review.go",
						OriginalPosition: 4,
						CreatedAt:        submittedAt,
						User:             &forgejo.User{Login: "alice"},
					},
				})
			case "/api/v1/repos/owner/repo/pulls/42/reviews/71/comments",
				"/api/v1/repos/owner/repo/pulls/42/reviews/73/comments":
				writeGatewayJSON(t, w, http.StatusOK, []*forgejo.PullReviewComment{})
			case "/api/v1/repos/owner/repo/issues/comments/101":
				assert.Equal(t, http.MethodPatch, r.Method)
				assertGatewayJSONBody(t, r, `{"body":"Edited comment."}`)
				writeGatewayJSON(t, w, http.StatusOK, forgejo.Comment{
					ID:   101,
					Body: "Edited comment.",
				})
			default:
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
		}))
	defer srv.Close()

	repo := newTestRepository(t, srv)
	threads, err := sliceutil.CollectErr(
		repo.ListReviewThreads(t.Context(), &PR{Number: 42}),
	)
	require.NoError(t, err)
	require.Len(t, threads, 2)

	assert.Equal(t, "review.go", threads[0].Path)
	assert.Equal(t, "42:70:review.go:3", threads[0].ID.String())
	assert.Equal(t, forge.ReviewThreadLine(3), threads[0].Range)
	assert.Equal(t, forge.ReviewThreadSideRight, threads[0].Side)
	assert.Equal(t, "1111111111111111111111111111111111111111", threads[0].CommitHash.String())
	require.NotNil(t, threads[0].Resolved)
	assert.True(t, *threads[0].Resolved)
	require.NotNil(t, threads[0].Outdated)
	assert.True(t, *threads[0].Outdated)
	require.Len(t, threads[0].Comments, 2)
	assert.Equal(t, "Root comment.", threads[0].Comments[0].Body)
	assert.Equal(t, "Reply comment.", threads[0].Comments[1].Body)

	assert.Equal(t, forge.ReviewThreadLine(4), threads[1].Range)
	assert.Equal(t, forge.ReviewThreadSideLeft, threads[1].Side)

	states, err := sliceutil.CollectErr(
		repo.ListReviewerStates(t.Context(), &PR{Number: 42}),
	)
	require.NoError(t, err)
	require.Len(t, states, 1)
	assert.Equal(t, "alice", states[0].Reviewer)
	assert.Equal(t, forge.ReviewDispositionApprove, states[0].Disposition)
	assert.Equal(t,
		git.Hash("2222222222222222222222222222222222222222"),
		states[0].CommitHash,
	)
	assert.Equal(t, updatedAt, states[0].SubmittedAt)

	require.NoError(t, repo.UpdateReviewComment(
		t.Context(), threads[0].Comments[0].ID, "Edited comment.",
	))
	_, resolves := any(repo).(forge.ReviewThreadResolver)
	assert.False(t, resolves)
}
