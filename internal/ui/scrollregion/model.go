// Package scrollregion runs a Bubble Tea model in a reserved terminal region.
//
// The package is motivated by command progress widgets,
// but it can wrap any Bubble Tea model that has a compact view
// and needs to coexist with ordinary terminal output.
// It disables Bubble Tea's renderer,
// draws the model itself into a bottom scroll region,
// and leaves normal writes to the output stream alone
// so logs can continue to print above the region.
//
// This is terminal-control code.
// It assumes an ANSI-compatible terminal with DECSTBM scrolling margins.
// When the output does not expose a file descriptor,
// callers must provide an explicit initial size
// or the model will wait until a size is available.
package scrollregion

import (
	"cmp"
	"context"
	"io"
	"sync"

	tea "charm.land/bubbletea/v2"
	"go.abhg.dev/gs/internal/sigstack"
)

// Options configures optional reserved-region behavior.
type Options struct {
	// Width and Height are the initial terminal size.
	//
	// If either value is zero
	// and output exposes an Fd method,
	// the missing value is detected from the terminal.
	Width, Height int

	// MinHeight is the minimum number of rows reserved for the model.
	//
	// Defaults to 1.
	// This prevents the reserved region from bouncing when a model
	// temporarily renders fewer rows.
	MinHeight int

	// MaxHeight is the maximum number of rows reserved for the model.
	//
	// Defaults to MinHeight.
	// This caps future model detail sections so log output keeps enough room.
	MaxHeight int

	// Signals receives terminal resize signals.
	//
	// If nil, a private stack is used for the lifetime of this model.
	Signals *sigstack.Stack
}

// Model wraps a Bubble Tea model with reserved-region rendering.
//
// Model is still a Bubble Tea model.
// Callers should pass it to tea.NewProgram with tea.WithoutRenderer,
// then call Start with that program before Run.
type Model struct {
	model    tea.Model
	renderer *renderer
	signals  *sigstack.Stack

	resizeMu     sync.Mutex
	cancelResize context.CancelFunc
	resizeDone   <-chan struct{}
}

var _ tea.Model = (*Model)(nil)

// New wraps model so it renders in a reserved terminal region.
//
// The output writer is the stream that receives both the wrapped model
// and any ordinary terminal output that should scroll above it.
// If opts is nil, defaults are used for all optional fields.
func New(model tea.Model, output io.Writer, opts *Options) *Model {
	opts = cmp.Or(opts, &Options{})
	r := newRenderer(
		output,
		opts.Width,
		opts.Height,
		opts.MinHeight,
		opts.MaxHeight,
	)
	signals := opts.Signals
	if signals == nil {
		signals = new(sigstack.Stack)
	}
	return &Model{
		model:    model,
		renderer: r,
		signals:  signals,
	}
}

// Close restores terminal margins and clears the reserved region.
func (m *Model) Close() error {
	m.stopResize()
	return m.renderer.Close()
}

// Start begins sending WindowSizeMsg values when the terminal resizes.
//
// Bubble Tea does not initialize terminal output
// while WithoutRenderer is in use,
// so this model owns resize watching for the reserved-region mode.
// Start is idempotent.
// Close stops the resize watcher before restoring the terminal.
func (m *Model) Start(program *tea.Program) {
	m.resizeMu.Lock()
	defer m.resizeMu.Unlock()

	if m.cancelResize != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelResize = cancel
	m.resizeDone = m.watchResize(ctx, program)
}

// Init initializes the wrapped model and sends the startup terminal size.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.renderer.initialWindowSize,
		m.model.Init(),
	)
}

// Update forwards messages to the wrapped model and renders the result.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.renderer.Resize(size.Width, size.Height)
		// The wrapped model should lay itself out within the reserved region,
		// not the full terminal height that log output uses above it.
		msg = tea.WindowSizeMsg{
			Width:  size.Width,
			Height: m.renderer.ModelHeight(),
		}
	}

	model, cmd := m.model.Update(msg)
	m.model = model
	m.renderer.Render(m.model.View())
	return m, cmd
}

// View returns an empty view because this model renders itself.
func (m *Model) View() tea.View {
	return tea.NewView("")
}

func (m *Model) stopResize() {
	m.resizeMu.Lock()
	cancel := m.cancelResize
	done := m.resizeDone
	m.cancelResize = nil
	m.resizeDone = nil
	m.resizeMu.Unlock()

	if cancel == nil {
		return
	}
	cancel()
	<-done
}
