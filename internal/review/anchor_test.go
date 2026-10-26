package review_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/review"
)

func TestAnchorUnmarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    review.Anchor
		wantErr string
	}{
		{
			name:  "File",
			input: "main.go",
			want:  review.Anchor{Path: "main.go"},
		},
		{
			name:  "Line",
			input: "main.go:42",
			want: review.Anchor{
				Path:      "main.go",
				StartLine: 42,
				EndLine:   42,
			},
		},
		{
			name:  "Range",
			input: "main.go:42-50",
			want: review.Anchor{
				Path:      "main.go",
				StartLine: 42,
				EndLine:   50,
			},
		},
		{
			name:  "ColonInPath",
			input: "path:main.go:42",
			want: review.Anchor{
				Path:      "path:main.go",
				StartLine: 42,
				EndLine:   42,
			},
		},
		{name: "Empty", wantErr: "comment anchor is required"},
		{name: "EmptyPath", input: ":42", wantErr: "path is required"},
		{name: "InvalidLine", input: "main.go:nope", wantErr: "invalid line number"},
		{name: "ZeroLine", input: "main.go:0", wantErr: "lines must be positive"},
		{name: "DescendingRange", input: "main.go:50-42", wantErr: "end must not precede start"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got review.Anchor
			err := got.UnmarshalText([]byte(tt.input))
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.input, got.String())
		})
	}
}
