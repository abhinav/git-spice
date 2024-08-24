package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateStackNavigationComment(t *testing.T) {
	tests := []struct {
		name    string
		graph   []*stackedChange
		current int
		want    string
	}{
		{
			name: "Single",
			graph: []*stackedChange{
				{Change: _changeID("123"), Base: -1},
			},
			current: 0,
			want: joinLines(
				"- #123 ◀",
			),
		},
		{
			name: "Downstack",
			graph: []*stackedChange{
				{Change: _changeID("123"), Base: -1},
				{Change: _changeID("124"), Base: 0},
				{Change: _changeID("125"), Base: 1},
			},
			current: 2,
			want: joinLines(
				"- #123",
				"    - #124",
				"        - #125 ◀",
			),
		},
		{
			name: "Upstack/Linear",
			graph: []*stackedChange{
				{Change: _changeID("123"), Base: -1},
				{Change: _changeID("124"), Base: 0},
				{Change: _changeID("125"), Base: 1},
			},
			current: 0,
			want: joinLines(
				"- #123 ◀",
				"    - #124",
				"        - #125",
			),
		},
		{
			name: "Upstack/NonLinear",
			graph: []*stackedChange{
				{Change: _changeID("123"), Base: -1},
				{Change: _changeID("124"), Base: 0}, // 1
				{Change: _changeID("125"), Base: 0}, // 2
				{Change: _changeID("126"), Base: 1},
				{Change: _changeID("127"), Base: 2},
			},
			current: 0,
			want: joinLines(
				"- #123 ◀",
				"    - #124",
				"        - #126",
				"    - #125",
				"        - #127",
			),
		},
		{
			name: "MidStack",
			graph: []*stackedChange{
				{Change: _changeID("123"), Base: -1}, // 0
				{Change: _changeID("124"), Base: 0},  // 1
				{Change: _changeID("125"), Base: 1},  // 2
				{Change: _changeID("126"), Base: 0},  // 3
				{Change: _changeID("127"), Base: 3},  // 4
			},
			// 1 has a sibling (3), but that won't be shown
			// as it's not in the path to the current branch.
			current: 1,
			want: joinLines(
				"- #123",
				"    - #124 ◀",
				"        - #125",
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Connect the upstacks.
			// Easier to write the test cases this way.
			for i, n := range tt.graph {
				if n.Base == -1 {
					continue
				}
				tt.graph[n.Base].Aboves = append(tt.graph[n.Base].Aboves, i)
			}

			want := _commentHeader + "\n\n" +
				tt.want + "\n" +
				_commentFooter + "\n" +
				_commentMarker + "\n"
			got := generateStackNavigationComment(tt.graph, tt.current)
			assert.Equal(t, want, got)

			// Sanity check: All generated comments must match
			// these regular expressions.
			t.Run("Regexp", func(t *testing.T) {
				for _, re := range _navCommentRegexes {
					assert.True(t, re.MatchString(got), "regexp %q failed", re)
				}
			})
		})
	}
}

func TestNavigationCommentWhen_StringMarshal(t *testing.T) {
	tests := []struct {
		give string
		want navigationCommentWhen
		str  string
	}{
		{
			give: "true",
			want: navigationCommentAlways,
			str:  "true",
		},
		{
			give: "false",
			want: navigationCommentNever,
			str:  "false",
		},
		{
			give: "multiple",
			want: navigationCommentOnMultiple,
			str:  "multiple",
		},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			var got navigationCommentWhen
			require.NoError(t, got.UnmarshalText([]byte(tt.give)))
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.give, got.String())
		})
	}

	t.Run("unknown", func(t *testing.T) {
		var f navigationCommentWhen
		require.Error(t, f.UnmarshalText([]byte("unknown")))
		assert.Equal(t, "unknown", navigationCommentWhen(42).String())
	})
}

type _changeID string

func (s _changeID) String() string {
	return "#" + string(s)
}

func joinLines(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}
