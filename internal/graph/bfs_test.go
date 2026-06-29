package graph_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/graph"
)

func TestBreadthFirstSort(t *testing.T) {
	tests := []struct {
		name string

		nodes   []string
		parents map[string]string
		want    []string
	}{
		{
			name:  "Linear",
			nodes: []string{"child", "grandchild", "root"},
			parents: map[string]string{
				"child":      "root",
				"grandchild": "child",
			},
			want: []string{"root", "child", "grandchild"},
		},
		{
			name:  "MultipleRoots",
			nodes: []string{"root-b", "child-a", "root-a", "child-b"},
			parents: map[string]string{
				"child-a": "root-a",
				"child-b": "root-b",
			},
			want: []string{"root-b", "root-a", "child-b", "child-a"},
		},
		{
			name:  "DivergentChildren",
			nodes: []string{"third", "root", "first", "second"},
			parents: map[string]string{
				"first":  "root",
				"second": "root",
				"third":  "root",
			},
			want: []string{"root", "third", "first", "second"},
		},
		{
			name:  "DeeperDescendant",
			nodes: []string{"great-grandchild", "child", "root", "grandchild"},
			parents: map[string]string{
				"child":            "root",
				"grandchild":       "child",
				"great-grandchild": "grandchild",
			},
			want: []string{"root", "child", "grandchild", "great-grandchild"},
		},
		{
			name:  "BreadthFirstLayerOrder",
			nodes: []string{"D", "Y", "B", "A", "C", "X"},
			parents: map[string]string{
				"B": "A",
				"C": "A",
				"D": "B",
				"Y": "X",
			},
			want: []string{"A", "X", "B", "C", "Y", "D"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := graph.BreadthFirstSort(tt.nodes,
				func(_ int, n string) int {
					parent, ok := tt.parents[n]
					if !ok {
						return -1
					}
					return slices.Index(tt.nodes, parent)
				})

			assert.Equal(t, tt.want, got)
		})
	}
}
