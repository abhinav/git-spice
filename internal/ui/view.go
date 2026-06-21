package ui

import (
	"cmp"
	"errors"
	"io"
	"os"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"go.abhg.dev/gs/internal/sigstack"
)

// ErrPrompt indicates that we're not running in interactive mode.
var ErrPrompt = errors.New("not allowed to prompt for input")

// View provides access to the UI,
// allowing the application to send messages to the user,
// and in interactive mode, prompt for input.
type View interface {
	// Write posts messages to the user.
	//
	// These are typically rendered to Stderr
	// to allow piping Stdout to other processes.
	io.Writer

	// Theme reports the active terminal theme for this view.
	Theme() Theme
}

// InteractiveView is a view that allows prompting the user for input.
//
// Views don't have to implement this interface, but if they do,
// they can prompt the user for input.
type InteractiveView interface {
	View

	// Prompt prompts the user for input with the given interactive fields.
	Prompt(...Field) error
}

// ModelView is a view that can run a Bubble Tea model.
type ModelView interface {
	View

	// RunModel runs the given Bubble Tea model against this view.
	RunModel(tea.Model, *RunOptions) error
}

// Interactive reports whether the given view is interactive.
func Interactive(v View) bool {
	_, ok := v.(InteractiveView)
	return ok
}

// FileView is a non-interactive view that posts messages
// to the given file.
type FileView struct {
	w     io.Writer
	theme Theme
}

var _ View = (*FileView)(nil)

// NewFileView builds a non-interactive view for the given writer.
func NewFileView(w io.Writer) *FileView {
	detectOutput := w
	if ow, ok := w.(*OutputWriter); ok {
		detectOutput = ow.Unwrap()
	}

	cpw, ok := w.(*colorprofile.Writer)
	if !ok {
		cpw = colorprofile.NewWriter(detectOutput, os.Environ())
	}

	return &FileView{
		w:     cpw,
		theme: detectTheme(nil, detectOutput),
	}
}

func (fv *FileView) Write(p []byte) (int, error) {
	return fv.w.Write(p)
}

// Theme reports the active terminal theme for this view.
func (fv *FileView) Theme() Theme {
	return fv.theme
}

// TerminalView is a view that posts messages to the user's terminal
// and allows prompting for input.
type TerminalView struct {
	r io.Reader
	w io.Writer

	theme   func() Theme
	signals *sigstack.Stack
}

var _ InteractiveView = (*TerminalView)(nil)

// NewTerminalView builds an interactive view for the given streams.
func NewTerminalView(r io.Reader, w io.Writer) *TerminalView {
	detectOutput := w
	if ow, ok := w.(*OutputWriter); ok {
		detectOutput = ow.Unwrap()
	}

	return &TerminalView{
		r: r,
		w: w,
		theme: sync.OnceValue(func() Theme {
			return detectTheme(r, detectOutput)
		}),
	}
}

// WithSignals sets the signal stack used by terminal UI components.
//
// If WithSignals is not called,
// components that need signal handling create private stacks.
func (tv *TerminalView) WithSignals(signals *sigstack.Stack) *TerminalView {
	tv.signals = signals
	return tv
}

func (tv *TerminalView) Write(p []byte) (int, error) {
	return tv.w.Write(p)
}

// Theme reports the active terminal theme for this view.
func (tv *TerminalView) Theme() Theme {
	return tv.theme()
}

// Prompt prompts the user for input with the given interactive fields.
func (tv *TerminalView) Prompt(fields ...Field) error {
	return NewForm(fields...).Run(&FormRunOptions{
		Input:   tv.r,
		Output:  tv.w,
		Theme:   tv.theme(),
		Signals: tv.signals,
	})
}

// RunModel runs the given Bubble Tea model against the terminal.
func (tv *TerminalView) RunModel(
	model tea.Model,
	opts *RunOptions,
) error {
	opts = cmp.Or(opts, &RunOptions{})
	if opts.Input == nil {
		opts.Input = tv.r
	}
	if opts.Output == nil {
		opts.Output = tv.w
	}
	if opts.Theme == (Theme{}) {
		opts.Theme = tv.theme()
	}
	if opts.Signals == nil {
		opts.Signals = tv.signals
	}
	return RunModel(model, opts)
}
