package ui

import (
	"errors"
	"io"
	"os"

	"github.com/charmbracelet/colorprofile"
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
	cpw, ok := w.(*colorprofile.Writer)
	if !ok {
		cpw = colorprofile.NewWriter(w, os.Environ())
	}

	return &FileView{
		w:     cpw,
		theme: detectTheme(nil, w),
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

	theme Theme
}

var _ InteractiveView = (*TerminalView)(nil)

// NewTerminalView builds an interactive view for the given streams.
func NewTerminalView(r io.Reader, w io.Writer) *TerminalView {
	return &TerminalView{
		r:     r,
		w:     w,
		theme: detectTheme(r, w),
	}
}

func (tv *TerminalView) Write(p []byte) (int, error) {
	return tv.w.Write(p)
}

// Theme reports the active terminal theme for this view.
func (tv *TerminalView) Theme() Theme {
	return tv.theme
}

// Prompt prompts the user for input with the given interactive fields.
func (tv *TerminalView) Prompt(fields ...Field) error {
	return NewForm(fields...).Run(&FormRunOptions{
		Input:  tv.r,
		Output: tv.w,
		Theme:  tv.theme,
	})
}
