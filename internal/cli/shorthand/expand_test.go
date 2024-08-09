package shorthand_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/cli/shorthand"
)

func TestExpand(t *testing.T) {
	tests := []struct {
		name string
		src  shorthand.Source
		args []string
		want []string
	}{
		{
			name: "NoArgs",
			args: []string{},
			want: []string{},
		},
		{
			name: "NoShorthand",
			src:  shorthandMap{},
			args: []string{"foo", "bar"},
			want: []string{"foo", "bar"},
		},
		{
			name: "SingleMatch",
			src: shorthandMap{
				"foo": {"bar", "baz"},
			},
			args: []string{"foo", "qux"},
			want: []string{"bar", "baz", "qux"},
		},
		{
			name: "MultipleMatches",
			src: shorthandMap{
				"can": {"ca", "--no-edit"},
				"ca":  {"c", "--amend"},
				"c":   {"commit"},
			},
			args: []string{"can", "--all"},
			want: []string{"commit", "--amend", "--no-edit", "--all"},
		},
		{
			name: "DeleteArgument",
			src: shorthandMap{
				"foo": {"baz"},
				"baz": {},
			},
			args: []string{"foo"},
			want: []string{},
		},
		{
			name: "Sources",
			src: shorthand.Sources{
				shorthandMap{"foo": {"bar", "baz"}},
				shorthandMap{"bar": {"qux"}},
			},
			args: []string{"foo"},
			want: []string{"qux", "baz"},
		},
		{
			name: "Sources/Cooperative",
			src: shorthand.Sources{
				shorthandMap{"can": {"ca", "--no-edit"}},
				shorthandMap{"ca": {"c", "--amend"}},
				shorthandMap{"c": {"commit"}},
			},
			args: []string{"can", "--all"},
			want: []string{"commit", "--amend", "--no-edit", "--all"},
		},
		{
			name: "Sources/CooperativeReverse",
			src: shorthand.Sources{
				shorthandMap{"c": {"commit"}},
				shorthandMap{"ca": {"c", "--amend"}},
				shorthandMap{"can": {"ca", "--no-edit"}},
			},
			args: []string{"can", "--all"},
			want: []string{"commit", "--amend", "--no-edit", "--all"},
		},
		{
			name: "Sources/Delete",
			src: shorthand.Sources{
				shorthandMap{"foo": {"bar", "baz"}},
				shorthandMap{"bar": {}},
			},
			args: []string{"foo"},
			want: []string{"baz"},
		},
		{
			name: "Sources/NoMatch",
			src: shorthand.Sources{
				shorthandMap{"foo": {"bar", "baz"}},
				shorthandMap{"bar": {"qux"}},
			},
			args: []string{"qux"},
			want: []string{"qux"},
		},
		{
			name: "Sources/InfiniteLoop",
			src: shorthand.Sources{
				shorthandMap{"foo": {"bar"}},
				shorthandMap{"bar": {"foo"}},
			},
			args: []string{"foo"},
			want: []string{"foo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shorthand.Expand(tt.src, tt.args)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExpand_noInfiniteLoop(t *testing.T) {
	// It should not be possible to create an infinite loop
	// with mutually recursive shorthands.
	src := shorthandMap{
		"foo": {"bar", "baz"},
		"bar": {"foo", "qux"},
	}

	args := []string{"foo"}
	got := shorthand.Expand(src, args)
	assert.Equal(t, []string{"foo", "qux", "baz"}, got)
}

type shorthandMap map[string][]string

func (m shorthandMap) ExpandShorthand(s string) ([]string, bool) {
	if expanded, ok := m[s]; ok {
		return expanded, true
	}
	return nil, false
}
