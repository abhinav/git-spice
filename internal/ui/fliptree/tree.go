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
	"bytes"
	"fmt"
	"io"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"go.abhg.dev/gs/internal/ui"
)

// DefaultNodeMarker is the marker used for each node in the tree.
var DefaultNodeMarker = ui.NewStyle().SetString("□")

// DefaultScrollUpMarker is the marker shown
// when content is scrolled out above the viewport.
var DefaultScrollUpMarker = ui.NewStyle().
	Foreground(ui.Gray).
	SetString("▲▲▲")

// DefaultScrollDownMarker is the marker shown
// when content is scrolled out below the viewport.
var DefaultScrollDownMarker = ui.NewStyle().
	Foreground(ui.Gray).
	SetString("▼▼▼")

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
	Theme ui.Theme
	Style *Style[T]

	// Offset states the number of lines to skip before rendering the tree,
	// and Height states the maximum number of lines to render after that.
	//
	// Use these together to render a view of a larger tree.
	//
	// A height <= 0 indicates no limit.
	// Scroll markers are rendered for a height > 0.
	Offset, Height int
}

// Style configures the visual appearance of the tree.
type Style[T any] struct {
	// Joint defines how the connecting joints between nodes are rendered.
	Joint ui.Style

	// NodeMarker returns the style of the marker
	// placed next to each node based on the node's value.
	//
	// By default, all nodes are marked with [DefaultNodeMarker].
	NodeMarker func(T) ui.Style

	// ScrollUpMarker is shown above the viewport
	// when content exists above it.
	ScrollUpMarker ui.Style

	// ScrollDownMarker is shown below the viewport
	// when content exists below it.
	ScrollDownMarker ui.Style
}

// DefaultStyle returns the default style for rendering trees.
func DefaultStyle[T any]() *Style[T] {
	return &Style[T]{
		Joint: ui.NewStyle().Faint(true),
		NodeMarker: func(T) ui.Style {
			return DefaultNodeMarker
		},
		ScrollUpMarker:   DefaultScrollUpMarker,
		ScrollDownMarker: DefaultScrollDownMarker,
	}
}

// Write renders the tree of nodes in g.
func Write[T any](w io.Writer, g Graph[T], opts Options[T]) error {
	if opts.Style == nil {
		opts.Style = DefaultStyle[T]()
	}

	tw := treeWriter[T]{
		w:      bufio.NewWriter(w),
		g:      g,
		style:  newTreeStyle(opts.Style, opts.Theme),
		offset: max(0, opts.Offset),
		height: opts.Height,
	}
	for _, root := range g.Roots {
		if err := tw.writeTree(root, nil, nil); err != nil {
			return err
		}
	}

	if tw.truncatedBelow {
		if _, err := fmt.Fprintln(tw.w, tw.style.ScrollDownMarker.String()); err != nil {
			return err
		}
	}
	return tw.w.Flush()
}

type treeWriter[T any] struct {
	w *bufio.Writer
	g Graph[T]

	lineNum int

	// Number of rendered tree lines to skip before starting the viewport.
	offset int
	// Maximum number of tree content lines to write.
	// Zero or negative means no viewport limit.
	height int

	// Number of tree content lines written to the viewport,
	// excluding scroll markers.
	wroteLines int

	// Whether the top scroll marker has already been emitted.
	// This is shown once when offset > 0 and the first viewport line is written.
	wroteScrollUp bool

	// Whether tree content was cut off at the bottom.
	// This is true if height > 0,
	// and the number of lines to write exceeds the height limit.
	//
	// This informs the caller whether a bottom scroll marker
	// should be emitted after the tree is fully rendered.
	truncatedBelow bool
	// TODO: maybe we can fold this into the render loop

	style treeStyle[T]
}

type treeStyle[T any] struct {
	Joint            lipgloss.Style
	NodeMarker       func(T) lipgloss.Style
	ScrollUpMarker   lipgloss.Style
	ScrollDownMarker lipgloss.Style
}

func newTreeStyle[T any](s *Style[T], theme ui.Theme) treeStyle[T] {
	return treeStyle[T]{
		Joint: s.Joint.Resolve(theme),
		NodeMarker: func(v T) lipgloss.Style {
			return s.NodeMarker(v).Resolve(theme)
		},
		ScrollUpMarker:   s.ScrollUpMarker.Resolve(theme),
		ScrollDownMarker: s.ScrollDownMarker.Resolve(theme),
	}
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

	var lineBuffer bytes.Buffer
	firstLine := true
	for line := range strings.SplitSeq(tw.g.View(nodeValue), "\n") {
		lineBuffer.Reset()

		// The text may be multi-line.
		// Only the first line has a title marker.
		if firstLine {
			firstLine = false
			tw.pipes(&lineBuffer, path, lastJoint, titlePrefix)
		} else {
			tw.pipes(&lineBuffer, path, string(_vertical)+" ", bodyPrefix)
		}

		lineBuffer.WriteString(line)
		tw.writeLine(lineBuffer.Bytes())
	}

	return nil
}

// writeLine writes a line of the tree to the output,
// respecting the scroll offset and height limits.
//
// This performs necessary bookkeeping internally.
func (tw *treeWriter[T]) writeLine(line []byte) {
	lineNum := tw.lineNum
	tw.lineNum++

	if lineNum < tw.offset {
		return
	}

	if tw.height > 0 && tw.wroteLines >= tw.height {
		tw.truncatedBelow = true
		return
	}

	// If content above the viewport is getting cut off,
	// we need to add a scroll marker.
	if !tw.wroteScrollUp && tw.offset > 0 {
		_, _ = fmt.Fprintln(tw.w, tw.style.ScrollUpMarker.String())
		tw.wroteScrollUp = true
	}

	_, _ = tw.w.Write(line)
	_ = tw.w.WriteByte('\n')

	tw.wroteLines++
}

func (tw *treeWriter[T]) pipes(buf *bytes.Buffer, path []int, joint string, marker string) {
	if len(path) == 0 {
		return
	}

	style := tw.style.Joint

	// Everything up to the last child
	// needs just connecting pipes.
	for _, pos := range path[:len(path)-1] {
		if pos > 0 {
			buf.WriteString(
				style.Render(string(_vertical) + " "),
			)
		} else {
			buf.WriteString("  ")
		}
	}

	buf.WriteString(style.Render(joint))
	buf.WriteString(marker)
}

// CycleError is returned when a cycle is detected in the tree.
type CycleError struct {
	// Nodes that form the cycle.
	Nodes []int
}

func (e *CycleError) Error() string {
	return "cycle detected"
}
