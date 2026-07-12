package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_UserID(t *testing.T) {
	gateway := newResponseGateway(t, `{"data":{"user":{"id":"U_1"}}}`)
	id, err := gateway.UserID(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, ID("U_1"), id)
}

func TestGateway_TeamID(t *testing.T) {
	gateway := newResponseGateway(t, `{"data":{"organization":{"team":{"id":"T_1"}}}}`)
	id, err := gateway.TeamID(t.Context(), "acme", "platform")
	require.NoError(t, err)
	assert.Equal(t, ID("T_1"), id)
}
