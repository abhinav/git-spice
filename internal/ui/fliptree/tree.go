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
type Graph struct {
	// Roots are the nodes at which we want to start rendering the tree.
	// Each root will be rendered as a separate tree.
	Roots []string

	// View returns the text for the node.
	// The value may be multiline, and may have its own styling.
	View func(string) string

	// Edges returns the nodes that are directly reachable
	// from the given node.
	Edges func(string) []string
}

// Options configure the rendering of the tree.
type Options struct {
	Style *Style

	// If non-nil, this map will be filled with the positions
	// of each node in the rendered tree.
	//
	// The key is the node name, and the value is 0-indexed line number
	// in the rendered tree where the node appears.
	Offsets map[string]int
}

// Style configures the visual appearance of the tree.
type Style struct {
	// Joint defines how the connecting joints between nodes are rendered.
	Joint lipgloss.Style

	// NodeMarker returns the style of the marker
	// placed next to each node based on the node's value.
	//
	// By default, all nodes are marked with [DefaultNodeMarker].
	NodeMarker func(string) lipgloss.Style
}

// DefaultStyle returns the default style for rendering trees.
func DefaultStyle() *Style {
	return &Style{
		Joint: ui.NewStyle().Faint(true),
		NodeMarker: func(string) lipgloss.Style {
			return DefaultNodeMarker
		},
	}
}

// Write renders the tree of nodes in g.
func Write(w io.Writer, g Graph, opts Options) error {
	if opts.Style == nil {
		opts.Style = DefaultStyle()
	}

	setOffset := func(string, int) {}
	if opts.Offsets != nil {
		setOffset = func(node string, line int) {
			opts.Offsets[node] = line
		}
	}

	tw := treeWriter{
		w:         bufio.NewWriter(w),
		g:         g,
		style:     opts.Style,
		setOffset: setOffset,
	}
	for _, root := range g.Roots {
		if err := tw.writeTree(root, nil, nil); err != nil {
			return err
		}
	}
	return tw.w.Flush()
}

type treeWriter struct {
	w *bufio.Writer
	g Graph

	lineNum   int
	style     *Style
	setOffset func(string, int)
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
// values[i] is the value for path[i].
func (tw *treeWriter) writeTree(node string, path []int, values []string) error {
	// Infinite loops are possible.
	for i, p := range values {
		if p == node {
			path := append(slices.Clone(path), i)
			return &CycleError{Path: path}
		}
	}

	// Children render first.
	var hasChildren bool
	for i, child := range tw.g.Edges(node) {
		hasChildren = true
		if err := tw.writeTree(child, append(path, i), append(values, node)); err != nil {
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
	titlePrefix := tw.style.NodeMarker(node).String() + " "
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

	lines := strings.Split(tw.g.View(node), "\n")
	for idx, line := range lines {
		// The text may be multi-line.
		// Only the first line has a title marker.
		if idx == 0 {
			tw.pipes(path, lastJoint, titlePrefix)
			tw.setOffset(node, tw.lineNum)
		} else {
			tw.pipes(path, string(_vertical)+" ", bodyPrefix)
		}

		_, _ = tw.w.WriteString(line)
		_, _ = tw.w.WriteString("\n")
		tw.lineNum++
	}

	return nil
}

func (tw *treeWriter) pipes(path []int, joint string, marker string) {
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
	// Path from root node to the node that formed the cycle.
	Path []int
}

func (e *CycleError) Error() string {
	return "cycle detected"
}
