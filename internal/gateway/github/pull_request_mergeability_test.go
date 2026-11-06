package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_PullRequestMergeability(t *testing.T) {
	gateway := newResponseGateway(t, `{
		"data": {"node": {"mergeable": "MERGEABLE", "mergeStateStatus": "CLEAN"}}
	}`)
	got, err := gateway.PullRequestMergeability(t.Context(), "PR_1")
	require.NoError(t, err)
	assert.Equal(t, MergeableStateMergeable, got.Mergeable)
}
