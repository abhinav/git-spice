package ui

import (
	"errors"
	"io"
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
	W io.Writer // required
}

var _ View = (*FileView)(nil)

func (fv *FileView) Write(p []byte) (int, error) {
	return fv.W.Write(p)
}

// TerminalView is a view that posts messages to the user's terminal
// and allows prompting for input.
type TerminalView struct {
	// R is the input stream to read from.
	R io.Reader // required

	// W is the output stream to write to.
	W io.Writer // required
}

var _ InteractiveView = (*TerminalView)(nil)

func (tv *TerminalView) Write(p []byte) (int, error) {
	return tv.W.Write(p)
}

// Prompt prompts the user for input with the given interactive fields.
func (tv *TerminalView) Prompt(fields ...Field) error {
	return NewForm(fields...).Run(&FormRunOptions{
		Input:  tv.R,
		Output: tv.W,
	})
}
