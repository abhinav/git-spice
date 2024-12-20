package graph_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/graph"
)

func TestToposort(t *testing.T) {
	tests := []struct {
		name string

		give map[string][]string // parent -> children
		want []string
	}{
		{name: "Empty", want: []string{}},
		{
			name: "Linear",
			give: map[string][]string{
				"a": {"b"},
				"b": {"c"},
				"c": {"d"},
			},
			want: []string{"a", "b", "c", "d"},
		},
		{
			name: "Disjoint",
			give: map[string][]string{
				// a -> {b -> d, c}
				"a": {"b", "c"},
				"b": {"d"},

				// e -> {f, g}
				"e": {"f", "g"},
			},
			want: []string{
				"a", "b", "c", "d", "e", "f", "g",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodeSet := make(map[string]struct{})
			parents := make(map[string]string) // node -> parent
			for parent, children := range tt.give {
				nodeSet[parent] = struct{}{}
				for _, child := range children {
					if p, ok := parents[child]; ok {
						t.Fatalf("invalid test case: %q already has a parent: %q", child, p)
					}

					nodeSet[child] = struct{}{}
					parents[child] = parent
				}
			}

			nodes := slices.Sorted(maps.Keys(nodeSet))
			got := graph.Toposort(nodes, func(n string) (string, bool) {
				parent, ok := parents[n]
				return parent, ok
			})

			assert.Equal(t, tt.want, got)
		})
	}
}
