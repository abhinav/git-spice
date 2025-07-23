package scanutil

import (
	"bufio"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitNull(t *testing.T) {
	tests := []struct {
		name string
		give string
		want []string
	}{
		{
			name: "Empty",
		},
		{
			name: "SingleTokenNoNull",
			give: "hello",
			want: []string{"hello"},
		},
		{
			name: "SingleTokenTrailingNull",
			give: "hello\x00",
			want: []string{"hello"},
		},
		{
			name: "MultipleTokens",
			give: "hello\x00world\x00",
			want: []string{"hello", "world"},
		},
		{
			name: "MultipleTokensNoTrailingNull",
			give: "hello\x00world",
			want: []string{"hello", "world"},
		},
		{
			name: "EmpySections",
			give: "\x00\x00hello\x00\x00world\x00\x00",
			want: []string{"", "", "hello", "", "world", ""},
		},
		{
			name: "AllEmpty",
			give: "\x00\x00\x00",
			want: []string{"", "", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.give)
			scan := bufio.NewScanner(r)
			scan.Split(SplitNull)

			var tokens []string
			for scan.Scan() {
				tokens = append(tokens, scan.Text())
			}

			require.NoError(t, scan.Err())
			assert.Equal(t, tt.want, tokens)
		})
	}
}
