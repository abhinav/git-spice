package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_ChangeStatuses(t *testing.T) {
	gateway := newResponseGateway(t, `{
		"data": {"nodes": [{"state": "OPEN", "headRefOid": "abc"}]}
	}`)
	statuses, err := gateway.ChangeStatuses(t.Context(), []ID{"PR_1"})
	require.NoError(t, err)
	require.Len(t, statuses, 1)
	assert.Equal(t, PullRequestStateOpen, statuses[0].State)
}
