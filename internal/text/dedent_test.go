package text

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDedent(t *testing.T) {
	tests := []struct {
		name string
		give string
		want string
	}{
		{
			name: "empty",
			give: "",
			want: "",
		},
		{
			name: "no indent",
			give: "foo\nbar\nbaz",
			want: "foo\nbar\nbaz",
		},
		{
			name: "indent",
			give: "  foo\n" +
				"    bar\n" +
				"  baz",
			want: "foo\n" +
				"  bar\n" +
				"baz",
		},
		{
			name: "empty first and last line",
			give: `
			  foo
			    bar
			  baz
			`,
			want: "foo\n" +
				"  bar\n" +
				"baz",
		},
		{
			name: "empty line in the middle",
			give: "  foo\n" +
				"\n" +
				"    bar\n" +
				"  baz",
			want: "foo\n" +
				"\n" +
				"  bar\n" +
				"baz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Dedent(tt.give)
			assert.Equal(t, tt.want, got)
		})
	}
}
