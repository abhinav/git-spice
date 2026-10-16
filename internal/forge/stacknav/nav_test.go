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

func TestPrinter_simple(t *testing.T) {
	tests := []struct {
		name    string
		graph   []Item
		current int
		trunk   string
		want    string
	}{
		{
			name:    "Single",
			graph:   []Item{{value: "#123", base: -1}},
			current: 0,
			trunk:   "main",
			want: joinLines(
				"- #123 ◀",
				"- `main`",
			),
		},
		{
			name: "MidStack",
			graph: []Item{
				{value: "#123", base: -1},
				{value: "#124", base: 0},
				{value: "#125", base: 1},
			},
			current: 1,
			trunk:   "trunk",
			want: joinLines(
				"- #125",
				"- #124 ◀",
				"- #123",
				"- `trunk`",
			),
		},
		{
			name: "NoTrunk",
			graph: []Item{
				{value: "#123", base: -1},
				{value: "#124", base: 0},
			},
			current: 1,
			want: joinLines(
				"- #124 ◀",
				"- #123",
			),
		},
		{
			// Forks above the current change fall back to the tree layout.
			name: "Fork",
			graph: []Item{
				{value: "#123", base: -1}, // 0
				{value: "#124", base: 0},  // 1
				{value: "#125", base: 0},  // 2
				{value: "#126", base: 1},  // 3
			},
			current: 0,
			trunk:   "main",
			want: joinLines(
				"- #123 ◀",
				"    - #124",
				"        - #126",
				"    - #125",
			),
		},
		{
			// Forks off the downstack aren't shown,
			// so they don't prevent the simple layout.
			name: "DownstackFork",
			graph: []Item{
				{value: "#123", base: -1}, // 0
				{value: "#124", base: 0},  // 1
				{value: "#125", base: 0},  // 2
				{value: "#126", base: 1},  // 3
			},
			current: 3,
			trunk:   "main",
			want: joinLines(
				"- #126 ◀",
				"- #124",
				"- #123",
				"- `main`",
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got strings.Builder
			Print(&got, tt.graph, tt.current, &PrintOptions{
				Simple: true,
				Trunk:  tt.trunk,
			})
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
