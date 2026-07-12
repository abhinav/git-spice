package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_FindPullRequests(t *testing.T) {
	gateway := newResponseGateway(t, `{
		"data": {"repository": {"pullRequests": {"nodes": [{
			"id": "PR_1", "number": 1, "state": "OPEN"
		}]}}}
	}`)
	got, err := gateway.FindPullRequests(t.Context(), "acme", "repo", "topic", 10, []PullRequestState{PullRequestStateOpen})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, ID("PR_1"), got[0].ID)
}

func TestGateway_PullRequest(t *testing.T) {
	gateway := newResponseGateway(t, `{
		"data": {"repository": {"pullRequest": {
			"id": "PR_1", "number": 1, "state": "OPEN"
		}}}
	}`)
	got, err := gateway.PullRequest(t.Context(), "acme", "repo", 1)
	require.NoError(t, err)
	assert.Equal(t, ID("PR_1"), got.ID)
}
