package ui

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"go.abhg.dev/gs/internal/must"
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
	Error lipgloss.Style

	Title         lipgloss.Style
	Description   lipgloss.Style
	AcceptedTitle lipgloss.Style

	AcceptedField lipgloss.Style
}

// DefaultFormStyle is the default style for a [Form].
var DefaultFormStyle = FormStyle{
	Error:         lipgloss.NewStyle().Foreground(_redColor),
	Title:         _titleStyle,
	Description:   _descriptionStyle,
	AcceptedTitle: _acceptedTitleStyle,

	AcceptedField: lipgloss.NewStyle().Faint(true),
}

type acceptFieldMsg struct{}

// AcceptField is a [tea.Cmd] to accept the currently focused field.
//
// It should be returned by a field's [Update] method
// to accept the field and move to the next one.
func AcceptField() tea.Msg {
	return acceptFieldMsg{}
}

// Field is a single field in a form.
type Field interface {
	Update(msg tea.Msg) tea.Cmd
	View() string
	// FIXME: Refactor to View(io.Writer) error

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

// Run presents a single field to the user and blocks until
// it's accepted or canceled.
//
// This is a convenience function for forms with just one field.
func Run(f Field) error {
	return NewForm(f).Run()
}

// Form presents a series of fields for the user to fill.
type Form struct {
	KeyMap FormKeyMap
	Style  FormStyle

	fields   []Field
	accepted []string

	err     error
	focused int // index of the focused field
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

// Run runs the form and blocks until it's accepted or canceled.
// It returns a combination of all errors returned by the fields.
func (f *Form) Run() error {
	if _, err := tea.NewProgram(f).Run(); err != nil {
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
	return nil
}

// Update implements tea.Model.
func (f *Form) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case acceptFieldMsg:
		must.BeInRangef(f.focused, 0, len(f.fields),
			"focused field (%v) out of range", f.focused)

		// When a field is accepted, freeze its current view.
		var acceptedView strings.Builder
		f.renderField(&acceptedView, f.fields[f.focused], true)
		f.accepted = append(f.accepted, acceptedView.String())

		f.focused++
		if f.focused >= len(f.fields) {
			// Quitting here guarantees that we'll
			// never be out of bounds.
			return f, tea.Quit
		}

	case tea.KeyMsg:
		if key.Matches(msg, f.KeyMap.Cancel) {
			f.err = errors.New("user cancelled")
			return f, tea.Quit
		}
	}

	return f, f.fields[f.focused].Update(msg)
}

// View implements tea.Model.
func (f *Form) View() string {
	var s strings.Builder
	for _, accepted := range f.accepted {
		s.WriteString(f.Style.AcceptedField.Render(accepted))
		s.WriteString("\n")
	}

	if f.focused < len(f.fields) {
		f.renderField(&s, f.fields[f.focused], false)
	}

	return s.String()
}

func (f *Form) renderField(w io.Writer, field Field, accepted bool) {
	if title := field.Title(); title != "" {
		titleStyle := f.Style.Title
		if accepted {
			titleStyle = f.Style.AcceptedTitle
		}

		fmt.Fprintf(w, "%s: ", titleStyle.Render(title))
	}
	fmt.Fprint(w, field.View())
	if err := field.Err(); err != nil {
		fmt.Fprintf(w, "\n%s", f.Style.Error.Render(err.Error()))
	}
	if desc := field.Description(); !accepted && desc != "" {
		fmt.Fprintf(w, "\n%s", f.Style.Description.Render(desc))
	}
}
