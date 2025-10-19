// Package stacknav provides support for creating stack navigation comments
// and descriptions.
package stacknav

import (
	"fmt"
	"io"
)

const (
	// _marker is the marker to use for the current change.
	_marker = "◀"

	// indent is a Markdown itemized list indentation.
	_indent = "    "
)

// PrintOptions customizes the behavior of Print.
type PrintOptions struct {
	// Marker is the marker to use for the current change.
	// If empty, defaults to "◀".
	Marker string
}

// Node is a single item in the stack navigation list.
// It usually represents a change in the Forge.
type Node interface {
	// Value returns the text to display for the node.
	// This will be rendered verbatim.
	Value() string

	// BaseIdx returns the index of the node below this one.
	// Use -1 if this is the bottom-most node of its stack.
	//
	// If the value is not -1, it MUST be a valid index in the nodes list
	// or the program will panic.
	BaseIdx() int
}

// Print visualizes a stack of changes in a Forge
// using a Markdown itemized list.
//
// For example:
//
//	This change is part of the following stack:
//
//	- #123
//	  - #124 ◀
//	    - #125
//
// currentIdx is the index of the current node in the nodes list.
// It will be marked with [Printer.Marker].
//
// opts can be used to customize the behavior of Print.
// If opts is nil, default options are used.
//
// All Write errors are ignored. Use a Writer that doesn't fail.
func Print[N Node](w io.Writer, nodes []N, currentIdx int, opts *PrintOptions) {
	marker := _marker
	if opts != nil && opts.Marker != "" {
		marker = opts.Marker
	}
	// aboves[i] holds indexes of nodes that are above nodes[i].
	aboves := make([][]int, len(nodes))
	for idx, node := range nodes {
		baseIdx := node.BaseIdx()
		if baseIdx >= 0 {
			aboves[baseIdx] = append(aboves[baseIdx], idx)
		}
	}

	writeNode := func(nodeIdx, indent int) {
		node := nodes[nodeIdx]
		for range indent {
			_, _ = io.WriteString(w, _indent)
		}

		_, _ = fmt.Fprintf(w, "- %v", node.Value())
		if nodeIdx == currentIdx {
			_, _ = fmt.Fprintf(w, " %v", marker)
		}

		_, _ = io.WriteString(w, "\n")
	}

	// The graph is a DAG, so we don't expect cycles.
	// Guard against it anyway.
	visited := make([]bool, len(nodes))
	ok := func(i int) bool {
		if i < 0 || i >= len(nodes) || visited[i] {
			return false
		}
		visited[i] = true
		return true
	}

	// Write the downstacks, not including the current node.
	// This will change the indent level.
	// The downstacks leading up to the current branch are always linear.
	var indent int
	{
		var downstacks []int
		for base := nodes[currentIdx].BaseIdx(); ok(base); base = nodes[base].BaseIdx() {
			downstacks = append(downstacks, base)
		}

		// Reverse order to print from base to current.
		for i := len(downstacks) - 1; i >= 0; i-- {
			writeNode(downstacks[i], indent)
			indent++
		}
	}

	// For the upstacks, we'll need to traverse the graph
	// and recursively write the upstacks.
	// Indentation will increase for each subtree.
	var visit func(int, int)
	visit = func(nodeIdx, indent int) {
		if !ok(nodeIdx) {
			return
		}

		writeNode(nodeIdx, indent)
		for _, aboveIdx := range aboves[nodeIdx] {
			visit(aboveIdx, indent+1)
		}
	}

	// Current branch and its upstacks.
	visit(currentIdx, indent)
}
