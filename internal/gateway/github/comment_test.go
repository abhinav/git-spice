package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_PullRequestComments(t *testing.T) {
	gateway := newResponseGateway(t, `{
		"data": {"node": {"comments": {
			"pageInfo": {"endCursor": "next", "hasNextPage": true},
			"nodes": [{"id": "C_1", "body": "hello"}]
		}}}
	}`)
	page, err := gateway.PullRequestComments(t.Context(), "PR_1", 10, nil)
	require.NoError(t, err)
	assert.Equal(t, "next", page.EndCursor)
	require.Len(t, page.Nodes, 1)
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
