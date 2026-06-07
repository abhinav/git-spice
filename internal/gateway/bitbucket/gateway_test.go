package bitbucket

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnsupportedGateway_SetChangeDraft(t *testing.T) {
	err := UnsupportedGateway{}.SetChangeDraft(t.Context(), 42, true)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupported)
}
