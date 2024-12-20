package graph

import "go.abhg.dev/gs/internal/must"

// Toposort performs a topological sort of the given nodes.
// parent returns the parent of a node, or false if the node doesn't have one.
//
// Values returned by parents MUST be in nodes.
// The graph MUST NOT have a cycle.
func Toposort[N comparable](
	nodes []N,
	parent func(N) (N, bool),
) []N {
	topo := make([]N, 0, len(nodes))
	seen := make(map[N]struct{})
	var visit func(N)
	visit = func(n N) {
		if _, ok := seen[n]; ok {
			return
		}
		seen[n] = struct{}{}

		if p, ok := parent(n); ok {
			visit(p)
		}

		topo = append(topo, n)
	}

	for _, n := range nodes {
		visit(n)
	}
	must.BeEqualf(len(nodes), len(topo),
		"topological sort produced incorrect number of elements:\n"+
			"nodes: %v\n"+
			"topo: %v", nodes, topo)

	return topo
}
