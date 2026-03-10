package ui

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// FormKeyMap defines the key bindings for a form.
// See [DefaultFormKeyMap] for default values.
type FormKeyMap struct {
	Cancel key.Binding
}

// DefaultFormKeyMap is the default key map for a [Form].
var DefaultFormKeyMap = FormKeyMap{
	Cancel: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "cancel"),
	),
}

// FormStyle configures the appearance of a [Form].
type FormStyle struct {
	Error Style

	Title         Style
	Description   Style
	AcceptedTitle Style

	AcceptedField Style
}

// DefaultFormStyle is the default style for a [Form].
var DefaultFormStyle = FormStyle{
	Error:         NewStyle().Foreground(Red),
	Title:         NewStyle().Foreground(Green).Bold(true),
	Description:   NewStyle().Foreground(Gray).Faint(true),
	AcceptedTitle: NewStyle().Foreground(Plain),
	AcceptedField: NewStyle().Faint(true),
}

type acceptFieldMsg struct{}

// AcceptField is a [tea.Cmd] to accept the currently focused field.
//
// It should be returned by a field's [Update] method
// to accept the field and move to the next one.
func AcceptField() tea.Msg {
	return acceptFieldMsg{}
}

type skipFieldMsg struct{}

// SkipField is a [tea.Cmd] to skip the currently focused field.
func SkipField() tea.Msg {
	return skipFieldMsg{}
}

// Writer receives a rendered view of a [Field].
type Writer interface {
	io.Writer
	io.StringWriter
}

// Field is a single field in a form.
type Field interface {
	// Init initializes the field.
	// This is called right before the field is first rendered,
	// not when the form is initialized.
	//
	// If this returns [SkipField], the field will be skipped.
	Init() tea.Cmd
	Update(msg tea.Msg) tea.Cmd
	Render(Writer, Theme)

	// UnmarshalValue unmarshals the field's value
	// using the given unmarshal function.
	//
	// The unmarhal function should be called with a pointer to a value
	// and it will attempt to decode the underlying raw value into it,
	// behaving similarly to encoding/json.Unmarshal.
	//
	// This function is used in tests to simulate user input.
	UnmarshalValue(unmarshal func(any) error) error

	// Err reports any errors for the field at render time.
	// These will be rendered in red below the field.
	//
	// It is the field's responsibility to ensure
	// that it does not post [AcceptField] while in an error state.
	Err() error

	// Title is a short title for the field.
	// This is always visible.
	Title() string

	// Description is a longer description of the field.
	// This is visible only while the field is focused.
	Description() string
}

// Run presents the given field to the user using the given View.
// If the view is not interactive, it will return an error.
func Run(v View, fs ...Field) error {
	iv, ok := v.(InteractiveView)
	if !ok {
		return ErrPrompt
	}

	return iv.Prompt(fs...)
}

// Form presents a series of fields for the user to fill.
type Form struct {
	KeyMap FormKeyMap
	Style  FormStyle

	fields   []Field
	accepted []string

	err     error
	focused int // index of the focused field
	theme   Theme
}

var _ tea.Model = (*Form)(nil)

// NewForm builds a new form with the given fields.
func NewForm(fields ...Field) *Form {
	return &Form{
		KeyMap: DefaultFormKeyMap,
		Style:  DefaultFormStyle,
		fields: fields,
	}
}

// FormRunOptions specifies options for [Form.Run].
type FormRunOptions struct {
	// Input is the input source.
	//
	// Defaults to os.Stdin.
	Input io.Reader

	// Output is the destination to write to.
	//
	// Defaults to os.Stderr.
	Output io.Writer

	// Theme is the active theme for the form.
	Theme Theme

	// SendMsg specifies a message that should be posted
	// to the program at startup.
	SendMsg tea.Msg

	// Width and Height specify the initial window size for the program.
	// If both are greater than zero, these are applied with tea.WithWindowSize.
	Width, Height int

	// TERM overrides the terminal type that Bubble Tea uses
	// for capability detection.
	TERM string

	// WithoutSignals requests that the form not register signal handlers.
	WithoutSignals bool
}

// Run runs the form and blocks until it's accepted or canceled.
// It returns a combination of all errors returned by the fields.
func (f *Form) Run(opts *FormRunOptions) error {
	opts = cmp.Or(opts, &FormRunOptions{})
	f.theme = opts.Theme

	var teaOpts []tea.ProgramOption
	if i := opts.Input; i != nil {
		teaOpts = append(teaOpts, tea.WithInput(i))
	}
	if o := opts.Output; o != nil {
		teaOpts = append(teaOpts, tea.WithOutput(o))
	}
	if opts.Width > 0 && opts.Height > 0 {
		teaOpts = append(teaOpts, tea.WithWindowSize(opts.Width, opts.Height))
	}
	if opts.TERM != "" {
		teaOpts = append(teaOpts,
			tea.WithEnvironment(append(os.Environ(), "TERM="+opts.TERM)))
	}
	if opts.WithoutSignals {
		teaOpts = append(teaOpts, tea.WithoutSignals())
	}

	prog := tea.NewProgram(f, teaOpts...)
	if msg := opts.SendMsg; msg != nil {
		go prog.Send(msg)
	}
	if _, err := prog.Run(); err != nil {
		return err
	}

	return f.Err()
}

// Err reports any errors that occurred during the form's execution
// or from any of the fields.
func (f *Form) Err() error {
	var errs []error
	if f.err != nil {
		errs = append(errs, f.err)
	}

	for _, field := range f.fields {
		if err := field.Err(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// Init initializes the form.
func (f *Form) Init() tea.Cmd {
	f.focused = 0
	if len(f.fields) == 0 {
		return tea.Quit
	}

	return f.fields[f.focused].Init()
}

// Update implements tea.Model.
func (f *Form) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	oldFocused := f.focused

	switch msg := msg.(type) {
	case acceptFieldMsg:
		if f.focused >= len(f.fields) {
			return f, tea.Quit
		}

		// When a field is accepted, freeze its current view.
		var acceptedView strings.Builder
		f.renderField(&acceptedView, f.fields[f.focused], true)
		f.accepted = append(f.accepted, acceptedView.String())
		f.focused++

	case skipFieldMsg:
		f.focused++

	case tea.KeyMsg:
		if key.Matches(msg, f.KeyMap.Cancel) {
			f.err = errors.New("user cancelled")
			return f, tea.Quit
		}
	}

	if f.focused >= len(f.fields) {
		return f, tea.Quit
	}

	if oldFocused != f.focused {
		return f, f.fields[f.focused].Init()
	}

	return f, f.fields[f.focused].Update(msg)
}

// Render renders the form to a string.
func (f *Form) Render() string {
	style := f.Style

	var s strings.Builder
	for _, accepted := range f.accepted {
		s.WriteString(style.AcceptedField.Render(f.theme, accepted))
		s.WriteString("\n")
	}

	if f.focused < len(f.fields) {
		f.renderField(&s, f.fields[f.focused], false)
	}

	return s.String()
}

// View implements tea.Model.
func (f *Form) View() tea.View {
	return tea.NewView(f.Render())
}

func (f *Form) renderField(w Writer, field Field, accepted bool) {
	style := f.Style

	if title := field.Title(); title != "" {
		titleStyle := style.Title
		if accepted {
			titleStyle = style.AcceptedTitle
		}

		fmt.Fprintf(w, "%s: ", titleStyle.Render(f.theme, title))
	}
	field.Render(w, f.theme)
	if err := field.Err(); err != nil {
		fmt.Fprintf(w, "\n%s", style.Error.Render(f.theme, err.Error()))
	}
	if desc := field.Description(); !accepted && desc != "" {
		fmt.Fprintf(w, "\n%s", style.Description.Render(f.theme, desc))
	}
}
