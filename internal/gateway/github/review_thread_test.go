package github

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_PullRequestReviewThreadCounts(t *testing.T) {
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
			assert.Equal(t, "query($ids:[ID!]!){nodes(ids: $ids){... on PullRequest{reviewThreads(first: 2){totalCount,pageInfo{endCursor,hasNextPage},nodes{isResolved}}}}}", request.Query)
			assert.JSONEq(t, `{
				"ids": ["PR_1", "PR_2"]
			}`, string(request.Variables))
			response = `{
				"data": {"nodes": [
					{"reviewThreads": {
						"totalCount": 3,
						"nodes": [{"isResolved": true}, {"isResolved": false}],
						"pageInfo": {"endCursor": "next", "hasNextPage": true}
					}},
					{"reviewThreads": {
						"totalCount": 1,
						"nodes": [{"isResolved": false}],
						"pageInfo": {"endCursor": "", "hasNextPage": false}
					}}
				]}
			}`
		case 2:
			assert.Equal(t, "query($after:String!$first:Int!$id:ID!){node(id: $id){... on PullRequest{reviewThreads(first: $first, after: $after){totalCount,pageInfo{endCursor,hasNextPage},nodes{isResolved}}}}}", request.Query)
			assert.JSONEq(t, `{
				"after": "next",
				"first": 2,
				"id": "PR_1"
			}`, string(request.Variables))
			response = `{
				"data": {"node": {"reviewThreads": {
					"totalCount": 3,
					"nodes": [{"isResolved": true}],
					"pageInfo": {"endCursor": "", "hasNextPage": false}
				}}}
			}`
		default:
			t.Fatalf("unexpected request %d", requestNum)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(response)),
		}, nil
	}))
	got, err := gateway.PullRequestReviewThreadCounts(
		t.Context(), []ID{"PR_1", "PR_2"},
		&PaginationOptions{ItemsPerPage: 2},
	)
	require.NoError(t, err)
	assert.Equal(t, []*ReviewThreadCounts{
		{Total: 3, Resolved: 2},
		{Total: 1, Resolved: 0},
	}, got)
	assert.Equal(t, 2, requestNum)
}

func TestGateway_PullRequestReviewThreadCounts_rejectsMismatchedNodeCount(t *testing.T) {
	gateway := newResponseGateway(t, `{
		"data": {"nodes": [
			{"reviewThreads": {
				"totalCount": 1,
				"nodes": [{"isResolved": true}],
				"pageInfo": {"endCursor": "", "hasNextPage": false}
			}}
		]}
	}`)

	_, err := gateway.PullRequestReviewThreadCounts(
		t.Context(), []ID{"PR_1", "PR_2"}, nil,
	)
	assert.ErrorContains(t, err, "got 1 nodes for 2 IDs")
}
