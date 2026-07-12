package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_LabelID(t *testing.T) {
	gateway := newResponseGateway(t, `{"data":{"repository":{"label":{"id":"L_1"}}}}`)
	id, err := gateway.LabelID(t.Context(), "acme", "repo", "bug")
	require.NoError(t, err)
	assert.Equal(t, ID("L_1"), id)
}

func TestGateway_CreateLabel(t *testing.T) {
	gateway := newResponseGateway(t, `{"data":{"createLabel":{"label":{"id":"L_1"}}}}`)
	id, err := gateway.CreateLabel(t.Context(), "R_1", "ffffff", "bug")
	require.NoError(t, err)
	assert.Equal(t, ID("L_1"), id)
}

func TestGateway_AddLabelsToLabelable(t *testing.T) {
	gateway := newResponseGateway(t, `{"data":{"addLabelsToLabelable":{}}}`)
	require.NoError(t, gateway.AddLabelsToLabelable(t.Context(), "PR_1", []ID{"L_1"}))
}

func TestGateway_DeleteLabel(t *testing.T) {
	gateway := newResponseGateway(t, `{"data":{"deleteLabel":{}}}`)
	require.NoError(t, gateway.DeleteLabel(t.Context(), "L_1"))
}
