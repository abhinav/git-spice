package shamhub

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestCLI_submitFeedbackSubmission(t *testing.T) {
	sh, getenv := newReviewCLITestShamHub(t)
	stdout := new(bytes.Buffer)
	reviewTime := "2026-08-22T12:00:00Z"

	require.NoError(t, runCLI(
		t.Context(),
		[]string{
			"review", "submit",
			"--reviewer", "alice",
			"--disposition", "approve",
			"--body", "Looks good.",
			"alice/example", "1",
		},
		reviewCLIEnvironment(getenv, reviewTime),
		stdout,
		new(bytes.Buffer),
	))
	assert.Empty(t, stdout.String())
	require.Len(t, sh.feedbackSubmissions, 1)
	assert.Equal(t, shamFeedbackSubmission{
		Change:      1,
		Submitter:   "alice",
		Disposition: forge.ReviewDispositionApprove,
		Body:        "Looks good.",
		CommentIDs:  []int{1},
		SubmittedAt: mustReviewTime(t, reviewTime),
	}, sh.feedbackSubmissions[0])
}

func TestCLI_postReviewComment(t *testing.T) {
	sh, getenv := newReviewCLITestShamHub(t)
	stdout := new(bytes.Buffer)

	require.NoError(t, runCLI(
		t.Context(),
		[]string{
			"review", "comment", "post",
			"--id", "100",
			"--author", "alice",
			"--path", "review.go",
			"--range", "3:5",
			"--side", "left",
			"--resolved",
			"--outdated",
			"alice/example", "1", "Needs attention.",
		},
		reviewCLIEnvironment(getenv, "2026-08-22T12:00:00Z"),
		stdout,
		new(bytes.Buffer),
	))
	assert.Equal(t, "100\n", stdout.String())
	require.Len(t, sh.comments, 1)
	assert.Equal(t, shamComment{
		ID:         100,
		Change:     1,
		Body:       "Needs attention.",
		Resolvable: true,
		Resolved:   true,
		Outdated:   true,
		CommitHash: "1111111111111111111111111111111111111111",
		Path:       "review.go",
		Line:       3,
		RangeStart: 3,
		RangeEnd:   5,
		Side:       forge.ReviewThreadSideLeft,
		ThreadID:   "thread-100",
		Author:     "alice",
		CreatedAt:  mustReviewTime(t, "2026-08-22T12:00:00Z"),
	}, sh.comments[0])

	stdout.Reset()
	require.NoError(t, runCLI(
		t.Context(),
		[]string{
			"review", "comment", "post",
			"--path", "review.go",
			"--range", "8",
			"alice/example", "1", "Later comment.",
		},
		reviewCLIEnvironment(getenv, "2026-08-22T12:00:00Z"),
		stdout,
		new(bytes.Buffer),
	))
	assert.Equal(t, "101\n", stdout.String())
}

func TestCLI_postReviewComment_fileLevel(t *testing.T) {
	sh, getenv := newReviewCLITestShamHub(t)
	stdout := new(bytes.Buffer)

	require.NoError(t, runCLI(
		t.Context(),
		[]string{
			"review", "comment", "post",
			"--id", "100",
			"--author", "alice",
			"--path", "review.go",
			"alice/example", "1", "File-level comment.",
		},
		reviewCLIEnvironment(getenv, "2026-08-22T12:00:00Z"),
		stdout,
		new(bytes.Buffer),
	))
	assert.Equal(t, "100\n", stdout.String())
	require.Len(t, sh.comments, 1)
	assert.Equal(t, shamComment{
		ID:         100,
		Change:     1,
		Body:       "File-level comment.",
		Resolvable: true,
		CommitHash: "1111111111111111111111111111111111111111",
		Path:       "review.go",
		ThreadID:   "thread-100",
		Author:     "alice",
		CreatedAt:  mustReviewTime(t, "2026-08-22T12:00:00Z"),
	}, sh.comments[0])
}

func TestCLI_replyReviewComment(t *testing.T) {
	sh, getenv := newReviewCLITestShamHub(t)
	sh.comments = append(sh.comments, shamComment{
		ID:         7,
		Change:     1,
		CommitHash: "1111111111111111111111111111111111111111",
		ThreadID:   "thread-7",
	})
	sh.changes[0].HeadHash = "2222222222222222222222222222222222222222"
	stdout := new(bytes.Buffer)

	require.NoError(t, runCLI(
		t.Context(),
		[]string{
			"review", "comment", "reply",
			"--author", "bob",
			"alice/example", "1", "thread-7", "Reply.",
		},
		reviewCLIEnvironment(getenv, "2026-08-22T12:00:00Z"),
		stdout,
		new(bytes.Buffer),
	))
	assert.Equal(t, "8\n", stdout.String())
	require.Len(t, sh.comments, 2)
	assert.Equal(t, shamComment{
		ID:         8,
		Change:     1,
		Body:       "Reply.",
		Resolvable: true,
		CommitHash: "1111111111111111111111111111111111111111",
		ThreadID:   "thread-7",
		Author:     "bob",
		CreatedAt:  mustReviewTime(t, "2026-08-22T12:00:00Z"),
	}, sh.comments[1])
}

func TestCLI_dumpFeedbackSubmissions(t *testing.T) {
	sh, getenv := newReviewCLITestShamHub(t)
	sh.feedbackSubmissions = append(sh.feedbackSubmissions,
		shamFeedbackSubmission{
			Change:      1,
			Submitter:   "alice",
			Disposition: forge.ReviewDispositionNone,
			Body:        "A comment submission.",
			CommentIDs:  []int{1},
		},
		shamFeedbackSubmission{
			Change:      1,
			Submitter:   "bob",
			Disposition: forge.ReviewDispositionRequestChanges,
			CommentIDs:  []int{2, 3},
		},
	)
	sh.comments = append(sh.comments,
		shamComment{ID: 1, Change: 1, Body: "A comment submission."},
		shamComment{
			ID:         2,
			Change:     1,
			Body:       "Root comment.",
			Resolvable: true,
			Path:       "review.go",
			Line:       3,
			RangeStart: 3,
			RangeEnd:   5,
			Side:       forge.ReviewThreadSideRight,
			ThreadID:   "thread-2",
			Author:     "bob",
		},
		shamComment{
			ID:         3,
			Change:     1,
			Body:       "Reply.",
			Resolvable: true,
			ThreadID:   "thread-2",
			Author:     "alice",
		},
		shamComment{
			ID:         4,
			Change:     1,
			Body:       "File-level comment.",
			Resolvable: true,
			Path:       "docs.go",
			ThreadID:   "thread-4",
			Author:     "carol",
		},
	)
	stdout := new(bytes.Buffer)

	require.NoError(t, runCLI(
		t.Context(),
		[]string{"dump", "reviews", "1"},
		getenv,
		stdout,
		new(bytes.Buffer),
	))
	assert.Equal(t, `changes:
  - change: 1
    submissions:
      - submitter: alice
        disposition: comment
        body: A comment submission.
        commentIDs:
          - 1
      - submitter: bob
        disposition: request-changes
        commentIDs:
          - 2
          - 3
    threads:
      - id: thread-2
        path: review.go
        range:
          start: 3
          end: 5
        side: right
        resolved: false
        outdated: false
        comments:
          - id: 2
            author: bob
            body: Root comment.
          - id: 3
            author: alice
            body: Reply.
      - id: thread-4
        path: docs.go
        resolved: false
        outdated: false
        comments:
          - id: 4
            author: carol
            body: File-level comment.
`, stdout.String())
}

func newReviewCLITestShamHub(t *testing.T) (*ShamHub, func(string) string) {
	t.Helper()

	sh, err := New(Config{Log: silogtest.New(t)})
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, sh.Close())
	})
	seedMergeabilityChange(sh)
	sh.changes[0].HeadHash = "1111111111111111111111111111111111111111"
	return sh, func(key string) string {
		switch key {
		case "SHAMHUB_API_URL":
			return sh.APIURL()
		case "SHAMHUB_URL":
			return sh.GitURL()
		case "SHAMHUB_ADMIN_TOKEN":
			return sh.AdminToken()
		default:
			return ""
		}
	}
}

func reviewCLIEnvironment(getenv func(string) string, committerDate string) func(string) string {
	return func(key string) string {
		if key == "GIT_COMMITTER_DATE" {
			return committerDate
		}
		return getenv(key)
	}
}

func mustReviewTime(t *testing.T, value string) time.Time {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, value)
	require.NoError(t, err)
	return parsed
}
