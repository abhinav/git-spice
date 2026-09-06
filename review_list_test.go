package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/handler/review"
)

func TestReviewListJSONUnsupportedThreadState(t *testing.T) {
	var stdout bytes.Buffer
	err := writeReviewListJSON(
		&stdout,
		&review.LoadResult{
			Comments: []review.ListedComment{
				{
					Thread: forge.ReviewThread{
						ID:    testReviewThreadID("thread-1"),
						Path:  "review.go",
						Range: forge.ReviewThreadLine(3),
						Side:  forge.ReviewThreadSideRight,
					},
					Comment: forge.ReviewComment{
						ID:     testReviewCommentID("comment-1"),
						Body:   "Consider a constant.",
						Author: "reviewer",
					},
				},
			},
		},
	)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"kind": "forge",
		"id": "comment-1",
		"scope": "line",
		"path": "review.go",
		"line": 3,
		"side": "right",
		"body": "Consider a constant.",
		"threadID": "thread-1",
		"author": "reviewer",
		"status": "open"
	}`, stdout.String())
}

type testReviewThreadID string

func (id testReviewThreadID) String() string {
	return string(id)
}

type testReviewCommentID string

func (id testReviewCommentID) String() string {
	return string(id)
}
