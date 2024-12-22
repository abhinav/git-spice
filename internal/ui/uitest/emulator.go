package uitest

import (
	"cmp"
	"errors"
	"io"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
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
	stdinW io.Writer // nil if not running a prompt
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
	stdinR, stdinW := io.Pipe()
	defer func() {
		_ = stdinR.Close()
		e.mu.Lock()
		e.stdinW = nil
		e.mu.Unlock()
	}()

	e.mu.Lock()
	w, h := e.term.Width, e.term.Height
	e.stdinW = stdinW
	e.mu.Unlock()

	return ui.NewForm(fs...).Run(&ui.FormRunOptions{
		Input:  stdinR,
		Output: e,
		// In-memory terminal emulator cannot be queried for size,
		// so inject this manually.
		SendMsg: tea.WindowSizeMsg{
			Width:  w,
			Height: h,
		},
		WithoutSignals: true,
	})
}

// Write posts messages to the user.
func (e *EmulatorView) Write(p []byte) (n int, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.term.Write(p)
}

// Close closes the EmulatorView and frees its resources.
func (e *EmulatorView) Close() error {
	return nil // TODO: post EOT?
}

// FeedKeys feeds the given keys to the terminal emulator.
func (e *EmulatorView) FeedKeys(keys string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.stdinW == nil {
		return errors.New("no prompt to fill")
	}

	_, err := io.WriteString(e.stdinW, keys)
	return err
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
