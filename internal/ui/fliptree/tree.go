// Package fliptree renders a tree of nodes as text in reverse:
// children first, then parent.
//
// For example: given main -> {feat1 -> feat1.1, feat2 -> feat2.1}
// The tree would look like this:
//
//	  ┌── feat2.1
//	┌─┴ feat2
//	│ ┌── feat1.1
//	├─┴ feat1
//	main
package fliptree

import (
	"bufio"
	"io"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"go.abhg.dev/gs/internal/ui"
)

// DefaultNodeMarker is the marker used for each node in the tree.
var DefaultNodeMarker = lipgloss.NewStyle().SetString("□")

// Graph defines a directed graph.
type Graph[T any] struct {
	// Values specifies the value for each node in the graph.
	// All nodes must have a value.
	Values []T

	// Edges returns the nodes that are directly reachable
	// from the given node as indexes into the Values slice.
	Edges func(T) []int

	// Roots are indexes of nodes in the Values slice
	// at which we start rendering the tree.
	//
	// Each root will be rendered as a separate tree.
	Roots []int

	// View returns the text for the node.
	// The view for a node may be multiline, and may have its own styling.
	View func(T) string
}

// Options configure the rendering of the tree.
type Options[T any] struct {
	Style *Style[T]
}

// Style configures the visual appearance of the tree.
type Style[T any] struct {
	// Joint defines how the connecting joints between nodes are rendered.
	Joint lipgloss.Style

	// NodeMarker returns the style of the marker
	// placed next to each node based on the node's value.
	//
	// By default, all nodes are marked with [DefaultNodeMarker].
	NodeMarker func(T) lipgloss.Style
}

// DefaultStyle returns the default style for rendering trees.
func DefaultStyle[T any]() *Style[T] {
	return &Style[T]{
		Joint: ui.NewStyle().Faint(true),
		NodeMarker: func(T) lipgloss.Style {
			return DefaultNodeMarker
		},
	}
}

// Write renders the tree of nodes in g.
func Write[T any](w io.Writer, g Graph[T], opts Options[T]) error {
	if opts.Style == nil {
		opts.Style = DefaultStyle[T]()
	}

	tw := treeWriter[T]{
		w:     bufio.NewWriter(w),
		g:     g,
		style: opts.Style,
	}
	for _, root := range g.Roots {
		if err := tw.writeTree(root, nil, nil); err != nil {
			return err
		}
	}
	return tw.w.Flush()
}

type treeWriter[T any] struct {
	w *bufio.Writer
	g Graph[T]

	lineNum int
	style   *Style[T]
}

const (
	_vertical      boxRune = '┃'
	_horizontal    boxRune = '━'
	_horizontalUp  boxRune = '┻'
	_verticalRight boxRune = '┣'
	_downRight     boxRune = '┏'
)

type boxRune rune

func (b boxRune) String() string {
	return string(b)
}

func (b boxRune) Valid() bool {
	switch b {
	case _vertical, _horizontal, _horizontalUp, _verticalRight, _downRight:
		return true
	default:
		return false
	}
}

func (b boxRune) HasLeft() bool {
	switch b {
	case _horizontal, _horizontalUp:
		return true
	default:
		return false
	}
}

func (b boxRune) HasRight() bool {
	switch b {
	case _horizontal, _downRight, _verticalRight, _horizontalUp:
		return true
	default:
		return false
	}
}

func (b boxRune) HasUp() bool {
	switch b {
	case _vertical, _horizontalUp, _verticalRight:
		return true
	default:
		return false
	}
}

func (b boxRune) HasDown() bool {
	switch b {
	case _vertical, _verticalRight, _downRight:
		return true
	default:
		return false
	}
}

// writeTree renders a subtree of the tree rooted at node.
//
// path is the path from the root to the current node
// as a series of indexes of the children of each branch.
// For example, [0, 2, 1] means:
//
//	root.Edges[0].Edges[2].Edges[1]
//
// An empty path means the current node is the root.
//
// nodes[i] is the node for path[i], as an index into the Values slice.
func (tw *treeWriter[T]) writeTree(nodeIdx int, path []int, pathNodeIxes []int) error {
	// Infinite loops are possible.
	for _, n := range pathNodeIxes {
		if n == nodeIdx {
			return &CycleError{Nodes: append(slices.Clone(pathNodeIxes), n)}
		}
	}

	nodeValue := tw.g.Values[nodeIdx]

	// Children render first.
	var hasChildren bool
	for i, child := range tw.g.Edges(nodeValue) {
		hasChildren = true
		if err := tw.writeTree(child, append(path, i), append(pathNodeIxes, nodeIdx)); err != nil {
			return err
		}
	}

	// In the following:
	//   ┌─□ feat1.1
	//  ─┴□ feat1
	// Whether we use add an upwards joint depends on whether
	// the current node has children.
	// If it has children, then we need a connecting pipe
	// for the branch above.
	titlePrefix := tw.style.NodeMarker(nodeValue).String() + " "
	if hasChildren {
		titlePrefix = tw.style.Joint.Render(string(_horizontalUp)) + titlePrefix
	}
	bodyPrefix := strings.Repeat(" ", lipgloss.Width(titlePrefix))

	lastJoint := string(_downRight) + string(_horizontal)
	if len(path) > 0 && path[len(path)-1] > 0 {
		// If pos > 0, then we've drawn siblings
		// above this branch, so we need a connecting pipe.
		// Otherwise, this is the topmost branch,
		// so we need no connecting pipe.
		lastJoint = string(_verticalRight) + string(_horizontal)
	}

	lines := strings.Split(tw.g.View(nodeValue), "\n")
	for idx, line := range lines {
		// The text may be multi-line.
		// Only the first line has a title marker.
		if idx == 0 {
			tw.pipes(path, lastJoint, titlePrefix)
		} else {
			tw.pipes(path, string(_vertical)+" ", bodyPrefix)
		}

		_, _ = tw.w.WriteString(line)
		_, _ = tw.w.WriteString("\n")
		tw.lineNum++
	}

	return nil
}

func (tw *treeWriter[T]) pipes(path []int, joint string, marker string) {
	if len(path) == 0 {
		return
	}

	style := tw.style.Joint

	// Everything up to the last child
	// needs just connecting pipes.
	for _, pos := range path[:len(path)-1] {
		if pos > 0 {
			_, _ = tw.w.WriteString(
				style.Render(string(_vertical) + " "),
			)
		} else {
			_, _ = tw.w.WriteString("  ")
		}
	}

	_, _ = tw.w.WriteString(style.Render(joint) + marker)
}

// CycleError is returned when a cycle is detected in the tree.
type CycleError struct {
	// Nodes that form the cycle.
	Nodes []int
}

func (e *CycleError) Error() string {
	return "cycle detected"
}
