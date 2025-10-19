package ui

import (
	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// InputKeyMap defines the key bindings for an input field.
type InputKeyMap struct {
	Accept key.Binding
	Up     key.Binding
	Down   key.Binding
}

// DefaultInputKeyMap is the default key map for an input field.
var DefaultInputKeyMap = InputKeyMap{
	Accept: key.NewBinding(
		key.WithKeys("enter", "tab"),
		key.WithHelp("enter/tab", "accept"),
	),
	Up: key.NewBinding(
		key.WithKeys("up"),
		key.WithHelp("up", "previous option"),
	),
	Down: key.NewBinding(
		key.WithKeys("down"),
		key.WithHelp("down", "next option"),
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

	model   textinput.Model
	value   *string
	options []string
	current int // current index in options, or -1 if custom value
}

var _ Field = (*Input)(nil)

// NewInput builds a new input field.
func NewInput() *Input {
	m := textinput.New()
	m.Prompt = "" // we have our own prompt
	m.Cursor.SetMode(cursor.CursorStatic)
	return &Input{
		KeyMap:  DefaultInputKeyMap,
		Style:   DefaultInputStyle,
		model:   m,
		value:   new(string),
		current: -1,
	}
}

// WithValue sets the destination for the input field.
// If the value is non-empty, it will be used as the initial value.
func (i *Input) WithValue(value *string) *Input {
	i.value = value
	return i
}

// WithOptions sets the list of options that can be cycled through with arrow keys.
// The options are ordered from oldest to newest (chronologically).
func (i *Input) WithOptions(options []string) *Input {
	i.options = options
	return i
}

// UnmarshalValue reads a string value for the input field.
// Optionally, the input may be a boolean true to accept
// the input as is.
func (i *Input) UnmarshalValue(unmarshal func(any) error) error {
	if ok := new(bool); unmarshal(ok) == nil && *ok {
		return nil
	}

	return unmarshal(i.value)
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
	i.model.SetValue(*i.value)
	i.model.Err = nil

	// Find the current index if the initial value matches one of the options
	if len(i.options) > 0 {
		for idx, opt := range i.options {
			if opt == *i.value {
				i.current = idx
				break
			}
		}
		// If no match found, current stays at -1 (custom value)
	}

	return i.model.Focus()
}

// Update handles a bubbletea event.
func (i *Input) Update(msg tea.Msg) tea.Cmd {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(keyMsg, i.KeyMap.Accept):
			// Accept only if input is valid.
			if err := i.model.Err; err == nil {
				i.model.Blur()
				return AcceptField
			}
			return nil

		case key.Matches(keyMsg, i.KeyMap.Up):
			// Cycle to previous option
			if len(i.options) > 0 {
				if i.current == -1 {
					// Start from the newest (last) option
					i.current = len(i.options) - 1
				} else {
					i.current--
					if i.current < 0 {
						i.current = len(i.options) - 1
					}
				}
				i.model.SetValue(i.options[i.current])
				i.model.CursorEnd()
				*i.value = i.options[i.current]
				return nil
			}

		case key.Matches(keyMsg, i.KeyMap.Down):
			// Cycle to next option
			if len(i.options) > 0 {
				if i.current == -1 {
					// Start from the oldest (first) option
					i.current = 0
				} else {
					i.current++
					if i.current >= len(i.options) {
						i.current = 0
					}
				}
				i.model.SetValue(i.options[i.current])
				i.model.CursorEnd()
				*i.value = i.options[i.current]
				return nil
			}
		}
	}

	var cmd tea.Cmd
	i.model, cmd = i.model.Update(msg)
	newValue := i.model.Value()

	// If the user manually edited the text, mark as custom value
	if newValue != *i.value && (i.current == -1 || newValue != i.options[i.current]) {
		i.current = -1
	}

	*i.value = newValue
	return cmd
}

// Render renders the input field.
func (i *Input) Render(w Writer) {
	w.WriteString(i.model.View())
}
