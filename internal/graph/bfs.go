package graph

import "go.abhg.dev/gs/internal/must"

// BreadthFirstSort orders nodes by breadth-first dependency layers.
//
// parent returns the index of a node's parent in nodes,
// or -1 if the node doesn't have one.
// Callers own node identity,
// so nodes may contain values that are not comparable.
//
// BreadthFirstSort returns roots first,
// then direct children,
// then deeper descendants.
// Input order is the stable tie-breaker for roots and siblings.
func BreadthFirstSort[N any](
	nodes []N,
	parent func(idx int, node N) int,
) []N {
	children := make([][]int, len(nodes))
	roots := make([]int, 0, len(nodes))
	for idx, node := range nodes {
		parentIdx := parent(idx, node)
		if parentIdx == -1 {
			roots = append(roots, idx)
			continue
		}
		must.Bef(parentIdx >= 0 && parentIdx < len(nodes),
			"parent index %d for node index %d out of range", parentIdx, idx)
		children[parentIdx] = append(children[parentIdx], idx)
	}

	visited := make([]bool, len(nodes))
	queue := make([]int, 0, len(nodes))
	sorted := make([]N, 0, len(nodes))

	enqueue := func(idx int) {
		if visited[idx] {
			return
		}
		visited[idx] = true
		queue = append(queue, idx)
	}

	drain := func() {
		for len(queue) > 0 {
			idx := queue[0]
			queue = queue[1:]

			sorted = append(sorted, nodes[idx])
			for _, child := range children[idx] {
				enqueue(child)
			}
		}
	}

	for _, root := range roots {
		enqueue(root)
	}
	drain()

	return sorted
}
