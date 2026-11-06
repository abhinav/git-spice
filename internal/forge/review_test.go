package forge

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReviewThreadRange_IsZero(t *testing.T) {
	assert.True(t, (ReviewThreadRange{}).IsZero())
	assert.False(t, ReviewThreadLine(1).IsZero())
}

func TestReviewThreadLine(t *testing.T) {
	assert.Equal(t, ReviewThreadRange{
		StartLine: 42,
		EndLine:   42,
	}, ReviewThreadLine(42))
}

func TestReviewThreadSide_String(t *testing.T) {
	tests := []struct {
		name string
		give ReviewThreadSide
		want string
	}{
		{
			name: "Right",
			give: ReviewThreadSideRight,
			want: "right",
		},
		{
			name: "Left",
			give: ReviewThreadSideLeft,
			want: "left",
		},
		{
			name: "Unknown",
			give: ReviewThreadSide(42),
			want: "ReviewThreadSide(42)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.give.String())
		})
	}
}
