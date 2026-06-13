package scrollregion

import (
	"io"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"go.abhg.dev/gs/internal/must"
)

const defaultWidth = 80

// renderer owns the terminal scroll margins and reserved-region frames.
type renderer struct {
	// out is the same stream used by ordinary log output.
	//
	// If it exposes Fd, the renderer can discover terminal size from it.
	out io.Writer // immutable

	mu     sync.Mutex // guards mutable fields below
	width  int
	height int

	minHeight int // immutable
	maxHeight int // immutable

	// currentHeight is recalculated from the rendered view within the configured
	// bounds so a compact model does not permanently claim MaxHeight rows.
	currentHeight int

	// reserved is true after DECSTBM has been installed.
	//
	// The first reservation waits until Render sees real content so the
	// terminal does not flicker through a min-height region and then grow.
	reserved bool
	closed   bool
}

func newRenderer(
	out io.Writer,
	width int,
	height int,
	minHeight int,
	maxHeight int,
) *renderer {
	must.NotBeNilf(out, "scroll region output is required")
	if minHeight <= 0 {
		minHeight = 1
	}
	if maxHeight <= 0 {
		maxHeight = minHeight
	}
	must.Bef(minHeight <= maxHeight,
		"scroll region minimum height %d exceeds maximum height %d",
		minHeight, maxHeight)
	must.Bef(height <= 0 || minHeight < height,
		"scroll region minimum height %d must be less than terminal height %d",
		minHeight, height)

	return &renderer{
		out:           out,
		width:         width,
		height:        height,
		minHeight:     minHeight,
		maxHeight:     maxHeight,
		currentHeight: minHeight,
	}
}

// Resize updates the terminal dimensions used for the scroll margins.
//
// Resize may run before the first render. In that case it records the size
// but waits for Render to install the margins at the rendered content height.
func (r *renderer) Resize(width, height int) {
	if width <= 0 || height <= 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}

	if r.reserved && r.width == width && r.height == height {
		return
	}

	r.width = width
	r.height = height
	if r.currentHeight >= r.height {
		r.currentHeight = r.height - 1
	}
	if !r.reserved {
		return
	}
	r.reserveLocked(0)
}

// Render draws view into the reserved rows.
func (r *renderer) Render(view tea.View) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}
	if r.height <= 0 {
		return
	}

	lines := viewLines(view.Content)
	oldHeight := r.currentHeight
	r.currentHeight = r.heightForLines(len(lines))
	if !r.reserved {
		r.reserveInitial()
	} else if oldHeight != r.currentHeight {
		r.reserveLocked(oldHeight)
	}
	if len(lines) > r.currentHeight {
		lines = lines[:r.currentHeight]
	}

	var b strings.Builder
	renderStart := r.regionStart()
	if len(lines) < r.currentHeight {
		renderStart += r.currentHeight - len(lines)
	}
	for row := range r.currentHeight {
		b.WriteString(ansi.CursorPosition(1, r.regionStart()+row))
		b.WriteString(ansi.EraseLine(2))
		lineIdx := row - (renderStart - r.regionStart())
		if lineIdx >= 0 && lineIdx < len(lines) {
			b.WriteString(lines[lineIdx])
		}
	}
	// Park the cursor on the last scrollable row.
	// Ordinary log writes should resume above the reserved rows,
	// not inside the redrawn model region.
	b.WriteString(ansi.CursorPosition(1, r.scrollBottom()))
	_, _ = io.WriteString(r.out, b.String())
}

// Close clears the reserved rows and restores normal scrolling.
func (r *renderer) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}
	r.closed = true
	if !r.reserved {
		return nil
	}

	var b strings.Builder
	for row := range r.currentHeight {
		b.WriteString(ansi.CursorPosition(1, r.regionStart()+row))
		b.WriteString(ansi.EraseLine(2))
	}
	// Reset DECSTBM by omitting top and bottom margins.
	// github.com/charmbracelet/x/ansi provides SetScrollingRegion
	// but no reset helper in the version used by this module.
	b.WriteString("\x1b[r")
	b.WriteString(ansi.ShowCursor)
	b.WriteString(ansi.CursorPosition(1, r.height))
	_, err := io.WriteString(r.out, b.String())
	return err
}

// ModelHeight reports the current height presented to the wrapped model.
func (r *renderer) ModelHeight() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.currentHeight
}

// initialWindowSize builds the startup WindowSizeMsg for the wrapped model.
//
// Bubble Tea normally sends this message after initializing its renderer.
// Reserved-region mode disables that renderer, so the model synthesizes the
// same message from explicit options or the output file descriptor.
func (r *renderer) initialWindowSize() tea.Msg {
	width, height := r.size()

	if termWidth, termHeight, ok := r.latestWindowSize(); ok {
		if width <= 0 {
			width = termWidth
		}
		if height <= 0 {
			height = termHeight
		}
	}
	if width <= 0 {
		width = defaultWidth
	}
	if height <= 0 {
		return nil
	}
	return tea.WindowSizeMsg{Width: width, Height: height}
}

// latestWindowSize reads the current terminal size from the output stream.
func (r *renderer) latestWindowSize() (width, height int, ok bool) {
	fdOut, ok := r.out.(interface{ Fd() uintptr })
	if !ok {
		return 0, 0, false
	}

	width, height, err := term.GetSize(fdOut.Fd())
	if err != nil {
		return 0, 0, false
	}
	return width, height, true
}

func (r *renderer) size() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.width, r.height
}

func (r *renderer) reserveInitial() {
	var b strings.Builder
	// Print blank rows before installing DECSTBM so the terminal has physical
	// rows available for the reserved region at the bottom of the viewport.
	b.WriteString(strings.Repeat("\n", r.currentHeight))
	b.WriteString(r.marginSequence())
	b.WriteString(ansi.HideCursor)
	b.WriteString(ansi.CursorPosition(1, r.scrollBottom()))
	_, _ = io.WriteString(r.out, b.String())
	r.reserved = true
}

func (r *renderer) reserveLocked(oldHeight int) {
	clearHeight := max(oldHeight, r.currentHeight)
	var b strings.Builder
	b.WriteString(r.marginSequence())
	b.WriteString(ansi.HideCursor)
	if clearHeight > 0 {
		start := r.height - clearHeight + 1
		for row := range clearHeight {
			b.WriteString(ansi.CursorPosition(1, start+row))
			b.WriteString(ansi.EraseLine(2))
		}
	}
	// Park the cursor on the last scrollable row.
	// The next ordinary write should occur above the reserved rows,
	// not inside the redrawn model region.
	b.WriteString(ansi.CursorPosition(1, r.scrollBottom()))
	_, _ = io.WriteString(r.out, b.String())
}

func (r *renderer) marginSequence() string {
	// DECSTBM keeps ordinary log output scrolling above the reserved rows.
	return ansi.SetTopBottomMargins(1, r.scrollBottom())
}

func (r *renderer) scrollBottom() int {
	return r.height - r.currentHeight
}

func (r *renderer) regionStart() int {
	return r.scrollBottom() + 1
}

func (r *renderer) heightForLines(lines int) int {
	height := max(lines, r.minHeight)
	height = min(height, r.maxHeight)
	return min(height, r.height-1)
}

func viewLines(content string) []string {
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}
