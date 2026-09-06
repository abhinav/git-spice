package forge_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/forge"
)

func TestMergeOperationStatus(t *testing.T) {
	assert.Equal(t, "pending", forge.MergeOperationPending.String())
	assert.Equal(t, "accepted", forge.MergeOperationAccepted.String())
	assert.Equal(t, "MergeOperationPending", forge.MergeOperationPending.GoString())
	assert.Equal(t, "MergeOperationAccepted", forge.MergeOperationAccepted.GoString())
	assert.Equal(t, "MergeOperationStatus(42)", forge.MergeOperationStatus(42).String())
}
