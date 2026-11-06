package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_PullRequestID(t *testing.T) {
	gateway := newResponseGateway(t, `{
		"data": {"repository": {"pullRequest": {"id": "PR_1"}}}
	}`)
	id, err := gateway.PullRequestID(t.Context(), "acme", "repo", 1)
	require.NoError(t, err)
	assert.Equal(t, ID("PR_1"), id)
}
