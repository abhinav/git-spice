package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestZeroHash(t *testing.T) {
	assert.Len(t, ZeroHash, 40, "ZeroHash is not 40 characters long")
	assert.True(t, ZeroHash.IsZero(), "ZeroHash is not zero")
}
