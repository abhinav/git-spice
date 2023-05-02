package ioutil

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPrintfWriter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc   string
		prefix string
		writes []string
		want   []string
	}{
		{desc: "empty"},
		{
			desc:   "single line",
			writes: []string{"hello world"},
			want:   []string{"hello world"},
		},
		{
			desc:   "single line/prefix",
			prefix: "prefix: ",
			writes: []string{"hello world"},
			want:   []string{"prefix: hello world"},
		},
		{
			desc:   "single line/newline",
			writes: []string{"hello world\n"},
			want:   []string{"hello world"},
		},
		{
			desc:   "single line/newline and prefix",
			prefix: "prefix: ",
			writes: []string{"hello world\n"},
			want:   []string{"prefix: hello world"},
		},
		{
			desc:   "multi line",
			writes: []string{"foo\n", "bar\n"},
			want:   []string{"foo", "bar"},
		},
		{
			desc:   "newline with separate write",
			writes: []string{"foo", "\n", "bar\n"},
			want:   []string{"foo", "bar"},
		},
		{
			desc:   "line across many writes",
			writes: []string{"f", "oo\nb", "ar\nb", "az\n"},
			want:   []string{"foo", "bar", "baz"},
		},
		{
			desc: "empty line",
			writes: []string{
				"foo\n",
				"\n",
				"bar\n",
			},
			want: []string{"foo", "", "bar"},
		},
		{
			desc:   "empty line/prefix",
			prefix: "prefix: ",
			writes: []string{
				"foo\n",
				"\n",
				"bar\n",
			},
			want: []string{"prefix: foo", "prefix: ", "prefix: bar"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.desc, func(t *testing.T) {
			t.Parallel()

			var got []string
			w, flush := newPrintfWriter(
				func(format string, args ...any) {
					got = append(got, fmt.Sprintf(format, args...))
				},
				tt.prefix,
			)

			for _, s := range tt.writes {
				fmt.Fprint(w, s)
			}
			flush()

			assert.Equal(t, tt.want, got)
		})
	}
}
