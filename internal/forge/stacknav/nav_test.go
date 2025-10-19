package stacknav

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPrinter(t *testing.T) {
	tests := []struct {
		name    string
		graph   []Item
		current int
		want    string
	}{
		{
			name: "Single",
			graph: []Item{
				{value: "#123", base: -1},
			},
			current: 0,
			want: joinLines(
				"- #123 ◀",
			),
		},
		{
			name: "Downstack",
			graph: []Item{
				{value: "#123", base: -1},
				{value: "#124", base: 0},
				{value: "#125", base: 1},
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
			graph: []Item{
				{value: "#123", base: -1},
				{value: "#124", base: 0},
				{value: "#125", base: 1},
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
			graph: []Item{
				{value: "#123", base: -1},
				{value: "#124", base: 0}, // 1
				{value: "#125", base: 0}, // 2
				{value: "#126", base: 1},
				{value: "#127", base: 2},
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
			graph: []Item{
				{value: "#123", base: -1}, // 0
				{value: "#124", base: 0},  // 1
				{value: "#125", base: 1},  // 2
				{value: "#126", base: 0},  // 3
				{value: "#127", base: 3},  // 4
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
			var got strings.Builder
			Print(&got, tt.graph, tt.current, nil)
			assert.Equal(t, tt.want, got.String())
		})
	}
}

type Item struct {
	value string
	base  int
}

func (i Item) Value() string { return i.value }
func (i Item) BaseIdx() int  { return i.base }

func joinLines(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}
