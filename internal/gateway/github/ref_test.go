package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_RefExists(t *testing.T) {
	gateway := newResponseGateway(t, `{
		"data": {"repository": {"ref": {"name": "refs/heads/main"}}}
	}`)
	got, err := gateway.RefExists(t.Context(), "acme", "repo", "refs/heads/main")
	require.NoError(t, err)
	assert.True(t, got)
}
