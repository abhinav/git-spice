package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_ChangeTemplates(t *testing.T) {
	gateway := newResponseGateway(t, `{
		"data": {"repository": {"pullRequestTemplates": [
			{"filename": "template.md", "body": "body"}
		]}}
	}`)
	got, err := gateway.ChangeTemplates(t.Context(), "acme", "repo")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "template.md", got[0].Filename)
}
