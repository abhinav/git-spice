package graph

// FurthestChildren returns every node at the greatest depth reachable from
// root.
// It returns root when root has no children.
//
// The children callback must report direct children in deterministic traversal
// order.
// FurthestChildren preserves that order among results.
// The caller must provide an acyclic tree;
// cycles are not detected.
func FurthestChildren[N any](root N, children func(N) []N) []N {
	furthest := []N{root}
	for {
		var next []N
		for _, node := range furthest {
			next = append(next, children(node)...)
		}
		if len(next) == 0 {
			return furthest
		}
		furthest = next
	}
}
