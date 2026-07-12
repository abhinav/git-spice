package github

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_PullRequestComments(t *testing.T) {
	requestNum := 0
	gateway := newTestGateway(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Query     string          `json:"query"`
			Variables json.RawMessage `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))

		requestNum++
		var response string
		switch requestNum {
		case 1:
			assert.Equal(t, "query($after:String$first:Int!$id:ID!){node(id: $id){... on PullRequest{comments(first: $first, after: $after){pageInfo{endCursor,hasNextPage},nodes{id,body,url,viewerCanUpdate,viewerDidAuthor,createdAt,updatedAt}}}}}", request.Query)
			assert.JSONEq(t, `{
				"after": null,
				"first": 10,
				"id": "PR_1"
			}`, string(request.Variables))
			response = `{
				"data": {"node": {"comments": {
					"pageInfo": {"endCursor": "next", "hasNextPage": true},
					"nodes": [{"id": "C_1", "body": "hello"}]
				}}}
			}`
		case 2:
			assert.Equal(t, "query($after:String!$first:Int!$id:ID!){node(id: $id){... on PullRequest{comments(first: $first, after: $after){pageInfo{endCursor,hasNextPage},nodes{id,body,url,viewerCanUpdate,viewerDidAuthor,createdAt,updatedAt}}}}}", request.Query)
			assert.JSONEq(t, `{
				"after": "next",
				"first": 10,
				"id": "PR_1"
			}`, string(request.Variables))
			response = `{
				"data": {"node": {"comments": {
					"pageInfo": {"endCursor": "", "hasNextPage": false},
					"nodes": [{"id": "C_2", "body": "world"}]
				}}}
			}`
		default:
			t.Fatalf("unexpected request %d", requestNum)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(response)),
		}, nil
	}))

	var comments []*Comment
	for comment, err := range gateway.PullRequestComments(t.Context(), "PR_1", nil) {
		require.NoError(t, err)
		comments = append(comments, comment)
	}
	require.Len(t, comments, 2)
	assert.Equal(t, ID("C_1"), comments[0].ID)
	assert.Equal(t, ID("C_2"), comments[1].ID)
	assert.Equal(t, 2, requestNum)
}

func TestGateway_PullRequestComments_yieldsContinuationError(t *testing.T) {
	want := errors.New("continuation unavailable")
	requestNum := 0
	gateway := newTestGateway(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		requestNum++
		if requestNum == 2 {
			return nil, want
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body: io.NopCloser(strings.NewReader(`{
				"data": {"node": {"comments": {
					"pageInfo": {"endCursor": "next", "hasNextPage": true},
					"nodes": [{"id": "C_1", "body": "hello"}]
				}}}
			}`)),
		}, nil
	}))

	var comments []*Comment
	var gotErr error
	for comment, err := range gateway.PullRequestComments(t.Context(), "PR_1", nil) {
		if err != nil {
			gotErr = err
			continue
		}
		comments = append(comments, comment)
	}
	require.Len(t, comments, 1)
	assert.ErrorIs(t, gotErr, want)
	assert.ErrorContains(t, gotErr, "list comments (page 2)")
}

func TestGateway_AddComment(t *testing.T) {
	gateway := newResponseGateway(t, `{
		"data": {"addComment": {"commentEdge": {"node": {
			"id": "C_1", "url": "https://example.com/c"
		}}}}
	}`)
	comment, err := gateway.AddComment(t.Context(), "PR_1", "hello")
	require.NoError(t, err)
	assert.Equal(t, ID("C_1"), comment.ID)
}

func TestGateway_UpdateIssueComment(t *testing.T) {
	gateway := newResponseGateway(t, `{"data":{"updateIssueComment":{}}}`)
	require.NoError(t, gateway.UpdateIssueComment(t.Context(), "C_1", "updated"))
}

func TestGateway_DeleteIssueComment(t *testing.T) {
	gateway := newResponseGateway(t, `{"data":{"deleteIssueComment":{}}}`)
	require.NoError(t, gateway.DeleteIssueComment(t.Context(), "C_1"))
}
