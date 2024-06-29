package ui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// InputKeyMap defines the key bindings for an input field.
type InputKeyMap struct {
	Accept key.Binding
}

// DefaultInputKeyMap is the default key map for an input field.
var DefaultInputKeyMap = InputKeyMap{
	Accept: key.NewBinding(
		key.WithKeys("enter", "tab"),
		key.WithHelp("enter/tab", "accept"),
	),
}

// InputStyle defines the styles for an input field.
type InputStyle struct{}

// DefaultInputStyle is the default style for an input field.
var DefaultInputStyle = InputStyle{}

// Input is a text input field.
// It accepts a single line of text.
type Input struct {
	KeyMap InputKeyMap
	Style  InputStyle

	title string
	desc  string

	model textinput.Model
	value *string
}

var _ Field = (*Input)(nil)

// NewInput builds a new input field.
func NewInput() *Input {
	m := textinput.New()
	m.Prompt = "" // we have our own prompt
	return &Input{
		KeyMap: DefaultInputKeyMap,
		Style:  DefaultInputStyle,
		model:  m,
		value:  new(string),
	}
}

// WithValue sets the destination for the input field.
// If the value is non-empty, it will be used as the initial value.
func (i *Input) WithValue(value *string) *Input {
	i.value = value
	i.model.SetValue(*value)
	return i
}

// WithTitle sets the title of the input field.
func (i *Input) WithTitle(title string) *Input {
	i.title = title
	return i
}

// Title returns the title of the input field.
func (i *Input) Title() string {
	return i.title
}

// WithDescription sets the description of the input field.
func (i *Input) WithDescription(desc string) *Input {
	i.desc = desc
	return i
}

// Description returns the description of the input field.
func (i *Input) Description() string {
	return i.desc
}

// Err reports any errors encountered during the operation.
// The error is nil if the input was accepted.
func (i *Input) Err() error {
	return i.model.Err
}

// WithValidate sets a validation function for the input field.
//
// The field will not accept the input until the validation function
// returns nil.
func (i *Input) WithValidate(f func(string) error) *Input {
	i.model.Validate = f
	return i
}

// Init initializes the field.
func (i *Input) Init() tea.Cmd {
	i.model.Err = nil
	return i.model.Focus()
}

// Update handles a bubbletea event.
func (i *Input) Update(msg tea.Msg) tea.Cmd {
	if keyMsg, ok := msg.(tea.KeyMsg); ok && key.Matches(keyMsg, i.KeyMap.Accept) {
		// Accept only if input is valid.
		if err := i.model.Err; err == nil {
			i.model.Blur()
			return AcceptField
		}
	}

	var cmd tea.Cmd
	i.model, cmd = i.model.Update(msg)
	*i.value = i.model.Value()
	return cmd
}

// Render renders the input field.
func (i *Input) Render(w Writer) {
	w.WriteString(i.model.View())
}
