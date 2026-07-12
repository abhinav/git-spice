package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_PullRequestReviewThreads(t *testing.T) {
	gateway := newResponseGateway(t, `{
		"data": {"nodes": [{"reviewThreads": {
			"totalCount": 1, "nodes": [{"isResolved": true}], "pageInfo": {}
		}}]}
	}`)
	got, err := gateway.PullRequestReviewThreads(t.Context(), []ID{"PR_1"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, []bool{true}, got[0].Nodes)
}

func TestGateway_PullRequestReviewThreadsPage(t *testing.T) {
	gateway := newResponseGateway(t, `{
		"data": {"node": {"reviewThreads": {
			"totalCount": 1, "nodes": [{"isResolved": false}], "pageInfo": {}
		}}}
	}`)
	got, err := gateway.PullRequestReviewThreadsPage(t.Context(), "PR_1", 100, "next")
	require.NoError(t, err)
	assert.Equal(t, []bool{false}, got.Nodes)
}
