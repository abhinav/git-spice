package github

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_PullRequestReviewThreads_paginatesThreadsAndComments(t *testing.T) {
	requestNum := 0
	gateway := newTestGateway(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Query     string         `json:"query"`
			Variables jsontext.Value `json:"variables"`
		}
		require.NoError(t, json.UnmarshalRead(r.Body, &request))

		requestNum++
		var response string
		switch requestNum {
		case 1:
			assert.Contains(t, request.Query, "reviewThreads(first: $first, after: $after)")
			assert.Contains(t, request.Query, "comments(first: $commentsFirst)")
			assert.Contains(t, request.Query, "subjectType")
			assert.JSONEq(t, `{"after":null,"commentsFirst":100,"first":10,"id":"PR_1"}`, string(request.Variables))
			response = `{"data":{"node":{"reviewThreads":{"pageInfo":{"endCursor":"threads-next","hasNextPage":true},"nodes":[{"id":"T_1","path":"a.go","subjectType":"LINE","diffSide":"LEFT","startDiffSide":"LEFT","line":9,"startLine":7,"originalLine":19,"originalStartLine":17,"isResolved":true,"isOutdated":false,"comments":{"pageInfo":{"endCursor":"comments-next","hasNextPage":true},"nodes":[{"id":"C_1","url":"https://example.com/c1","body":"first","author":{"login":"octo"},"createdAt":"2026-08-22T10:00:00Z"}]}}]}}}}`
		case 2:
			assert.Contains(t, request.Query, "comments(first: $first, after: $after)")
			assert.JSONEq(t, `{"after":"comments-next","first":100,"id":"T_1"}`, string(request.Variables))
			response = `{"data":{"node":{"comments":{"pageInfo":{"endCursor":"","hasNextPage":false},"nodes":[{"id":"C_2","url":"https://example.com/c2","body":"reply","author":{"login":"hubot"},"createdAt":"2026-08-22T11:00:00Z"}]}}}}`
		case 3:
			assert.Contains(t, request.Query, "reviewThreads(first: $first, after: $after)")
			assert.JSONEq(t, `{"after":"threads-next","commentsFirst":100,"first":10,"id":"PR_1"}`, string(request.Variables))
			response = `{"data":{"node":{"reviewThreads":{"pageInfo":{"endCursor":"","hasNextPage":false},"nodes":[{"id":"T_2","path":"b.go","subjectType":"FILE","diffSide":"RIGHT","line":null,"originalLine":null,"isResolved":false,"isOutdated":true,"comments":{"pageInfo":{"endCursor":"","hasNextPage":false},"nodes":[]}}]}}}}`
		default:
			t.Fatalf("unexpected request %d", requestNum)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(response)),
		}, nil
	}))

	var threads []*PullRequestReviewThread
	for thread, err := range gateway.PullRequestReviewThreads(t.Context(), "PR_1", &PaginationOptions{ItemsPerPage: 10}) {
		require.NoError(t, err)
		threads = append(threads, thread)
	}
	require.Len(t, threads, 2)
	assert.Equal(t, ID("T_1"), threads[0].ID)
	assert.Equal(t, DiffSideLeft, threads[0].DiffSide)
	assert.Equal(t, ReviewThreadSubjectTypeLine, threads[0].SubjectType)
	assert.Equal(t, 7, *threads[0].StartLine)
	require.Len(t, threads[0].Comments, 2)
	assert.Equal(t, ID("C_2"), threads[0].Comments[1].ID)
	assert.Equal(t, ReviewThreadSubjectTypeFile, threads[1].SubjectType)
	assert.Nil(t, threads[1].Line)
	assert.Equal(t, 3, requestNum)
}

func TestGateway_ReviewMutations(t *testing.T) {
	requestNum := 0
	gateway := newTestGateway(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Query     string         `json:"query"`
			Variables jsontext.Value `json:"variables"`
		}
		require.NoError(t, json.UnmarshalRead(r.Body, &request))

		requestNum++
		var response string
		switch requestNum {
		case 1:
			assert.Contains(t, request.Query, "addPullRequestReview")
			assert.JSONEq(t, `{"input":{"pullRequestId":"PR_1"}}`, string(request.Variables))
			response = `{"data":{"addPullRequestReview":{"pullRequestReview":{"id":"R_1"}}}}`
		case 2:
			assert.Contains(t, request.Query, "addPullRequestReviewThread")
			assert.JSONEq(t, `{"input":{"pullRequestReviewId":"R_1","path":"a.go","line":9,"side":"LEFT","startLine":7,"startSide":"LEFT","body":"first"}}`, string(request.Variables))
			response = `{"data":{"addPullRequestReviewThread":{"thread":{"id":"T_1","comments":{"nodes":[{"id":"C_1","url":"https://example.com/c1"}]}}}}}`
		case 3:
			assert.Contains(t, request.Query, "addPullRequestReviewThreadReply")
			assert.JSONEq(t, `{"input":{"pullRequestReviewThreadId":"T_1","pullRequestReviewId":"R_1","body":"reply"}}`, string(request.Variables))
			response = `{"data":{"addPullRequestReviewThreadReply":{"comment":{"id":"C_2","url":"https://example.com/c2"}}}}`
		case 4:
			assert.Contains(t, request.Query, "submitPullRequestReview")
			assert.JSONEq(t, `{"input":{"pullRequestReviewId":"R_1","event":"REQUEST_CHANGES","body":"summary"}}`, string(request.Variables))
			response = `{"data":{"submitPullRequestReview":{"pullRequestReview":{"id":"R_1"}}}}`
		case 5:
			assert.Contains(t, request.Query, "updatePullRequestReviewComment")
			assert.JSONEq(t, `{"input":{"pullRequestReviewCommentId":"C_2","body":"updated"}}`, string(request.Variables))
			response = `{"data":{"updatePullRequestReviewComment":{"pullRequestReviewComment":{"id":"C_2"}}}}`
		case 6:
			assert.Contains(t, request.Query, "resolveReviewThread")
			assert.JSONEq(t, `{"input":{"threadId":"T_1"}}`, string(request.Variables))
			response = `{"data":{"resolveReviewThread":{"thread":{"id":"T_1"}}}}`
		case 7:
			assert.Contains(t, request.Query, "unresolveReviewThread")
			assert.JSONEq(t, `{"input":{"threadId":"T_1"}}`, string(request.Variables))
			response = `{"data":{"unresolveReviewThread":{"thread":{"id":"T_1"}}}}`
		default:
			t.Fatalf("unexpected request %d", requestNum)
		}

		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(response))}, nil
	}))

	review, err := gateway.AddPullRequestReview(t.Context(), &AddPullRequestReviewInput{PullRequestID: "PR_1"})
	require.NoError(t, err)
	assert.Equal(t, ID("R_1"), review.ID)

	startLine := 7
	startSide := DiffSideLeft
	thread, err := gateway.AddPullRequestReviewThread(t.Context(), &AddPullRequestReviewThreadInput{
		PullRequestReviewID: review.ID,
		Path:                "a.go",
		Line:                9,
		Side:                DiffSideLeft,
		StartLine:           &startLine,
		StartSide:           &startSide,
		Body:                "first",
	})
	require.NoError(t, err)
	assert.Equal(t, ID("T_1"), thread.ID)
	assert.Equal(t, ID("C_1"), thread.Comment.ID)

	reply, err := gateway.AddPullRequestReviewThreadReply(t.Context(), &AddPullRequestReviewThreadReplyInput{
		PullRequestReviewThreadID: thread.ID,
		PullRequestReviewID:       review.ID,
		Body:                      "reply",
	})
	require.NoError(t, err)
	assert.Equal(t, ID("C_2"), reply.ID)

	require.NoError(t, gateway.SubmitPullRequestReview(t.Context(), &SubmitPullRequestReviewInput{
		PullRequestReviewID: review.ID,
		Event:               ReviewEventRequestChanges,
		Body:                "summary",
	}))
	require.NoError(t, gateway.UpdatePullRequestReviewComment(t.Context(), reply.ID, "updated"))
	require.NoError(t, gateway.ResolveReviewThread(t.Context(), thread.ID))
	require.NoError(t, gateway.UnresolveReviewThread(t.Context(), thread.ID))
	assert.Equal(t, 7, requestNum)
}

func TestGateway_PullRequestLatestOpinionatedReviews(t *testing.T) {
	gateway := newResponseGateway(t, `{"data":{"node":{"latestOpinionatedReviews":{"pageInfo":{"hasNextPage":false},"nodes":[{"author":{"login":"octo"},"state":"APPROVED","commit":{"oid":"1111111111111111111111111111111111111111"},"submittedAt":"2026-08-22T10:00:00Z"}]}}}}`)

	var reviews []*PullRequestLatestOpinionatedReview
	for review, err := range gateway.PullRequestLatestOpinionatedReviews(t.Context(), "PR_1", nil) {
		require.NoError(t, err)
		reviews = append(reviews, review)
	}
	require.Len(t, reviews, 1)
	assert.Equal(t, "octo", reviews[0].Author.Login)
	assert.Equal(t, ReviewStateApproved, reviews[0].State)
	assert.Equal(t, "1111111111111111111111111111111111111111", reviews[0].Commit.OID)
}

func TestGateway_DirectReviewCommentMutations(t *testing.T) {
	requestNum := 0
	gateway := newTestGateway(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Query     string         `json:"query"`
			Variables jsontext.Value `json:"variables"`
		}
		require.NoError(t, json.UnmarshalRead(r.Body, &request))

		requestNum++
		var response string
		switch requestNum {
		case 1:
			assert.Contains(t, request.Query, "addPullRequestReviewThread")
			assert.JSONEq(t, `{"input":{"pullRequestId":"PR_1","path":"a.go","line":9,"side":"RIGHT","body":"thread"}}`, string(request.Variables))
			response = `{"data":{"addPullRequestReviewThread":{"thread":{"id":"T_1","comments":{"nodes":[{"id":"C_1","url":"https://example.com/c1"}]}}}}}`
		case 2:
			assert.Contains(t, request.Query, "addPullRequestReviewThreadReply")
			assert.JSONEq(t, `{"input":{"pullRequestReviewThreadId":"T_1","body":"reply"}}`, string(request.Variables))
			response = `{"data":{"addPullRequestReviewThreadReply":{"comment":{"id":"C_2","url":"https://example.com/c2"}}}}`
		default:
			t.Fatalf("unexpected request %d", requestNum)
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(response))}, nil
	}))

	thread, err := gateway.AddPullRequestReviewThread(t.Context(), &AddPullRequestReviewThreadInput{
		PullRequestID: "PR_1", Path: "a.go", Line: 9, Side: DiffSideRight, Body: "thread",
	})
	require.NoError(t, err)
	assert.Equal(t, ID("T_1"), thread.ID)
	reply, err := gateway.AddPullRequestReviewThreadReply(t.Context(), &AddPullRequestReviewThreadReplyInput{
		PullRequestReviewThreadID: thread.ID, Body: "reply",
	})
	require.NoError(t, err)
	assert.Equal(t, ID("C_2"), reply.ID)
}

func TestGateway_FileReviewCommentMutation(t *testing.T) {
	gateway := newTestGateway(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Query     string         `json:"query"`
			Variables jsontext.Value `json:"variables"`
		}
		require.NoError(t, json.UnmarshalRead(r.Body, &request))

		assert.Contains(t, request.Query, "addPullRequestReviewThread")
		assert.JSONEq(t, `{"input":{"pullRequestId":"PR_1","path":"a.go","subjectType":"FILE","body":"thread"}}`, string(request.Variables))
		response := `{"data":{"addPullRequestReviewThread":{"thread":{"id":"T_1","comments":{"nodes":[{"id":"C_1","url":"https://example.com/c1"}]}}}}}`
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(response))}, nil
	}))

	thread, err := gateway.AddPullRequestReviewThread(t.Context(), &AddPullRequestReviewThreadInput{
		PullRequestID: "PR_1", Path: "a.go", SubjectType: ReviewThreadSubjectTypeFile, Body: "thread",
	})
	require.NoError(t, err)
	assert.Equal(t, ID("T_1"), thread.ID)
	assert.Equal(t, ID("C_1"), thread.Comment.ID)
}
