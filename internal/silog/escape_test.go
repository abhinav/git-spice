package silog

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMaybeQuote(t *testing.T) {
	tests := []struct {
		give string
		want string
	}{
		// Empty string should not be quoted
		{"", ""},

		// Normal strings should not be quoted
		{"hello", "hello"},
		{"world123", "world123"},
		{"foo-bar_baz", "foo-bar_baz"},
		{"unicode文字", "unicode文字"},
		{"emoji👍", "emoji👍"},

		// Whitespace-only strings should be quoted
		{" ", `" "`},
		{"  ", `"  "`},
		{"\t", `"\t"`},
		{" \t ", `" \t "`},

		// Strings with control characters should be quoted
		{"hello\nworld", `"hello\nworld"`},
		{"tab\there", `"tab\there"`},
		{"carriage\rreturn", `"carriage\rreturn"`},
		{"vertical\vtab", `"vertical\vtab"`},
		{"form\ffeed", `"form\ffeed"`},

		// Mixed content with control chars should be quoted
		{"normal text\nwith newline", `"normal text\nwith newline"`},
		{"text with\ttab", `"text with\ttab"`},

		// Normal strings with spaces should not be quoted
		{"hello world", "hello world"},
		{"multiple word string", "multiple word string"},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			got := MaybeQuote(tt.give)
			assert.Equal(t, tt.want, got)
		})
	}
}
