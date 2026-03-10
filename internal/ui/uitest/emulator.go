package uitest

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/vito/midterm"
	"go.abhg.dev/gs/internal/ui"
)

// EmulatorView is a [ui.InteractiveView] that renders to an in-memory
// terminal emulator, and allows interacting with it programmatically.
type EmulatorView struct {
	logf func(string, ...any)

	// TODO: v2
	// renderer tea.Renderer

	mu     sync.RWMutex
	term   *midterm.Terminal
	stdinc chan string
}

var _ ui.InteractiveView = (*EmulatorView)(nil)

// Theme reports the test terminal theme.
func (*EmulatorView) Theme() ui.Theme { return ui.DefaultThemeLight() }

// EmulatorViewOptions are options for creating an [EmulatorView].
type EmulatorViewOptions struct {
	// Dimensions of the terminal screen.
	// If not provided, defaults to 24 rows and 80 columns.
	Rows, Cols int

	// NoAutoResize disables automatic resizing of the terminal
	// as output is written to it.
	NoAutoResize bool

	// Log function to use, if any.
	Logf func(string, ...any)
}

// NewEmulatorView creates a new [EmulatorView] with the given dimensions.
//
// The EmulatorView must be closed with Close when done.
func NewEmulatorView(opts *EmulatorViewOptions) *EmulatorView {
	opts = cmp.Or(opts, &EmulatorViewOptions{})
	term := midterm.NewTerminal(
		cmp.Or(opts.Rows, 24),
		cmp.Or(opts.Cols, 80),
	)
	term.AutoResizeX = !opts.NoAutoResize
	term.AutoResizeY = !opts.NoAutoResize
	// Bubble Tea v2 renders assuming raw-terminal line-feed semantics.
	// Midterm defaults to cooked-mode behavior, where '\n' implies '\r\n',
	// which breaks relative cursor updates in the emulator.
	term.Raw = true

	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}

	return &EmulatorView{
		logf: logf,
		term: term,
	}
}

// Prompt prompts the user for input with the given interactive fields.
func (e *EmulatorView) Prompt(fs ...ui.Field) error {
	stdinc := make(chan string, 1) // buffered to avoid blocking
	stdinR, stdinW := io.Pipe()    // io.Pipe is blocking, so we need to buffer it
	go func() {
		for s := range stdinc {
			_, _ = io.WriteString(stdinW, s)
		}
	}()

	e.mu.Lock()
	w, h := e.term.Width, e.term.Height
	e.stdinc = stdinc
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.stdinc = nil
		e.mu.Unlock()

		_ = stdinR.Close()
		close(stdinc)
	}()

	return ui.NewForm(fs...).Run(&ui.FormRunOptions{
		Input:  stdinR,
		Output: e,
		Theme:  e.Theme(),
		Width:  w,
		Height: h,
		// Force xterm capabilities in emulator-backed tests.
		// With TERM unset, Bubble Tea falls back to insert-mode redraws
		// using CSI 4 h/l, which midterm does not emulate correctly.
		// Advertising xterm steers Bubble Tea to a redraw path that
		// preserves the real rendered text while remaining compatible
		// with the test terminal.
		TERM: "xterm-256color",
		// In-memory terminal emulator cannot be queried for size,
		// so inject this manually for models that expect a startup resize msg.
		SendMsg:        tea.WindowSizeMsg{Width: w, Height: h},
		WithoutSignals: true,
	})
}

// Write posts messages to the user.
func (e *EmulatorView) Write(p []byte) (n int, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	n = len(p)
	for len(p) > 0 {
		tab := bytes.IndexByte(p, '\t')
		if tab < 0 {
			_, err := e.term.Write(p)
			return n, err
		}

		if tab > 0 {
			if _, err := e.term.Write(p[:tab]); err != nil {
				return n, err
			}
		}

		// Bubble Tea v2 uses HT as a cursor-movement optimization during
		// redraws. Midterm expands tabs into spaces, which erases previously
		// rendered cells instead of just advancing the cursor.
		move := 8 - (e.term.Cursor.X % 8)
		if move == 0 {
			move = 8
		}
		e.term.MoveForward(move)
		p = p[tab+1:]
	}

	return n, nil
}

// Close closes the EmulatorView and frees its resources.
func (e *EmulatorView) Close() error {
	return nil // TODO: post EOT?
}

// FeedKeys feeds the given keys to the terminal emulator.
func (e *EmulatorView) FeedKeys(keys string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stdinc == nil {
		return errors.New("no prompt to feed keys to")
	}

	e.stdinc <- keys
	return nil
}

// Rows returns a list of rows in the terminal emulator.
func (e *EmulatorView) Rows() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var lines []string
	for _, row := range e.term.Content {
		row = trimRightWS(row)
		lines = append(lines, string(row))
	}

	// Trim trailing empty lines.
	for i := len(lines) - 1; i >= 0; i-- {
		if len(lines[i]) > 0 {
			lines = lines[:i+1]
			break
		}
	}

	return lines
}

// Snapshot returns the entire content of the terminal emulator as a string.
func (e *EmulatorView) Snapshot() string {
	var out strings.Builder
	for _, row := range e.Rows() {
		fmt.Fprintln(&out, row)
	}
	return strings.TrimRight(out.String(), " \t\n")
}

func trimRightWS(rs []rune) []rune {
	for i := len(rs) - 1; i >= 0; i-- {
		switch rs[i] {
		case 0, ' ', '\t', '\n':
			// next
		default:
			rs = rs[:i+1]

			// Midterm stores untouched cells as zero runes.
			// Those cells render as spaces, but mutating a subslice here would
			// write back into the terminal buffer, so copy only when
			// normalization is actually needed.
			if j := slices.Index(rs, 0); j >= 0 {
				rs = slices.Clone(rs)
				for k := j; k < len(rs); k++ {
					if rs[k] == 0 {
						rs[k] = ' '
					}
				}
				return rs
			}

			return rs
		}
	}
	return nil
}
