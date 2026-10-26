package graph_test

import (
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			got, err := graph.Toposort(nodes, func(n string) (string, bool) {
				parent, ok := parents[n]
				return parent, ok
			})
			require.NoError(t, err)

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestToposort_cycle(t *testing.T) {
	_, err := graph.Toposort([]string{"a", "b", "c"},
		func(n string) (string, bool) {
			switch n {
			case "a":
				return "c", true
			case "b":
				return "a", true
			case "c":
				return "b", true
			default:
				return "", false
			}
		})
	require.Error(t, err)

	var cycleErr *graph.CycleError[string]
	require.True(t, errors.As(err, &cycleErr))
	assert.Equal(t, []string{"a", "c", "b", "a"}, cycleErr.Path)
	assert.Equal(t, "cycle: a -> c -> b -> a", err.Error())
}
