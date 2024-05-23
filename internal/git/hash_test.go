package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestZeroHash(t *testing.T) {
	assert.Len(t, ZeroHash, 40, "ZeroHash is not 40 characters long")
	assert.True(t, ZeroHash.IsZero(), "ZeroHash is not zero")
}

func TestHashShort(t *testing.T) {
	tests := []struct {
		give Hash
		want string
	}{
		{"", ""},
		{"a", "a"},
		{ZeroHash, "0000000"},
		{"abcdef0123456789abcdef0123456789abcdef012", "abcdef0"},
	}

	for _, tt := range tests {
		t.Run(tt.give.String(), func(t *testing.T) {
			assert.Equal(t, tt.want, tt.give.Short())
		})
	}
}
