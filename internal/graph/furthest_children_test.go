package graph_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/graph"
)

func TestFurthestChildren(t *testing.T) {
	tests := []struct {
		name     string
		root     string
		children map[string][]string
		want     []string
	}{
		{
			name: "Leaf",
			root: "a",
			want: []string{"a"},
		},
		{
			name: "Linear",
			root: "a",
			children: map[string][]string{
				"a": {"b"},
				"b": {"c"},
			},
			want: []string{"c"},
		},
		{
			name: "Longest",
			root: "a",
			children: map[string][]string{
				"a": {"b", "c"},
				"c": {"d"},
			},
			want: []string{"d"},
		},
		{
			name: "StableTie",
			root: "a",
			children: map[string][]string{
				"a": {"c", "b"},
				"b": {"d"},
				"c": {"e"},
			},
			want: []string{"e", "d"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := graph.FurthestChildren(tt.root, func(node string) []string {
				return tt.children[node]
			})
			assert.Equal(t, tt.want, got)
		})
	}
}
