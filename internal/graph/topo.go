package graph

import (
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/must"
)

// Toposort performs a topological sort of the given nodes.
// parent returns the parent of a node, or false if the node doesn't have one.
//
// Values returned by parents MUST be in nodes.
// If the graph has a cycle,
// Toposort returns a CycleError.
func Toposort[N comparable](
	nodes []N,
	parent func(N) (N, bool),
) ([]N, error) {
	topo := make([]N, 0, len(nodes))

	// visitState tracks DFS progress:
	// unseen nodes have not been explored,
	// visiting nodes are on the current DFS path,
	// and visited nodes already have a fixed position in topo.
	type visitState uint8
	const (
		unseen visitState = iota
		visiting
		visited
	)

	seen := make(map[N]visitState, len(nodes))
	var path []N
	var visit func(N) error
	visit = func(n N) error {
		switch seen[n] {
		case visiting:
			for i, node := range path {
				if node == n {
					return &CycleError[N]{
						Path: append(path[i:], n),
					}
				}
			}
			return &CycleError[N]{Path: []N{n, n}}
		case visited:
			return nil
		}

		seen[n] = visiting
		path = append(path, n)

		if p, ok := parent(n); ok {
			if err := visit(p); err != nil {
				return err
			}
		}

		path = path[:len(path)-1]
		seen[n] = visited
		topo = append(topo, n)
		return nil
	}

	for _, n := range nodes {
		if err := visit(n); err != nil {
			return nil, err
		}
	}
	must.BeEqualf(len(nodes), len(topo),
		"topological sort produced incorrect number of elements:\n"+
			"nodes: %v\n"+
			"topo: %v", nodes, topo)

	return topo, nil
}

// CycleError reports the cycle that prevented a topological sort.
type CycleError[N comparable] struct {
	Path []N
}

func (e *CycleError[N]) Error() string {
	return "cycle: " + e.Format(" -> ")
}

// Format formats the cycle path with sep between each node.
func (e *CycleError[N]) Format(sep string) string {
	var out strings.Builder
	for i, node := range e.Path {
		if i > 0 {
			out.WriteString(sep)
		}
		fmt.Fprint(&out, node)
	}
	return out.String()
}
