package uitest

import (
	"cmp"
	"io"
	"strings"
	"sync"

	"github.com/vito/midterm"
	"go.abhg.dev/gs/internal/ui"
)

// EmulatorView is a [ui.InteractiveView] that renders to an in-memory
// terminal emulator, and allows interacting with it programmatically.
type EmulatorView struct {
	real  *ui.TerminalView
	stdin io.WriteCloser
	term  *lockedTerminal
}

var _ ui.InteractiveView = (*EmulatorView)(nil)

// EmulatorViewOptions are options for creating an [EmulatorView].
type EmulatorViewOptions struct {
	// Dimensions of the terminal screen.
	// If not provided, defaults to 24 rows and 80 columns.
	Rows, Cols int

	// NoAutoResize disables automatic resizing of the terminal
	// as output is written to it.
	NoAutoResize bool
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
	lockedTerm := newLockedTerminal(term)

	stdinR, stdinW := io.Pipe()

	return &EmulatorView{
		real: &ui.TerminalView{
			R: stdinR,
			W: lockedTerm,
		},
		term:  lockedTerm,
		stdin: stdinW,
	}
}

// Prompt prompts the user for input with the given interactive fields.
func (e *EmulatorView) Prompt(fs ...ui.Field) error {
	return e.real.Prompt(fs...)
}

// Write posts messages to the user.
func (e *EmulatorView) Write(p []byte) (n int, err error) {
	return e.real.Write(p)
}

// Close closes the EmulatorView and frees its resources.
func (e *EmulatorView) Close() error {
	return e.stdin.Close()
}

// Rows returns a list of rows in the terminal emulator.
func (e *EmulatorView) Rows() []string {
	return e.term.Rows()
}

// Screen returns a string representation of the terminal emulator.
func (e *EmulatorView) Screen() string {
	return e.term.Screen()
}

// FeedKeys feeds the given keys to the terminal emulator.
func (e *EmulatorView) FeedKeys(keys string) error {
	_, err := io.WriteString(e.stdin, keys)
	return err
}

type lockedTerminal struct {
	mu   sync.RWMutex
	term *midterm.Terminal
}

func newLockedTerminal(term *midterm.Terminal) *lockedTerminal {
	return &lockedTerminal{term: term}
}

func (l *lockedTerminal) Write(p []byte) (n int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.term.Write(p)
}

func (l *lockedTerminal) Rows() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var lines []string
	for _, row := range l.term.Content {
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

func (l *lockedTerminal) Screen() string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var s strings.Builder
	for _, row := range l.term.Content {
		row = trimRightWS(row)
		s.WriteString(string(row))
		s.WriteRune('\n')
	}

	return strings.TrimRight(s.String(), "\n")
}

func trimRightWS(rs []rune) []rune {
	for i := len(rs) - 1; i >= 0; i-- {
		switch rs[i] {
		case ' ', '\t', '\n':
			// next
		default:
			return rs[:i+1]
		}
	}
	return nil
}
