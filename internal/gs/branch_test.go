package gs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateBranchName(t *testing.T) {
	tests := []struct {
		give string
		want string
	}{
		{"Hello, World!", "hello-world"},
		{"Long message that should be truncated", "long-message-that-should-be"},
		{"1234 5678", "1234-5678"},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			got := GenerateBranchName(tt.give)
			assert.Equal(t, tt.want, got)
		})
	}
}
