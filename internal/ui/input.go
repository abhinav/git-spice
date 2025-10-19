package ui

import (
	"strings"

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

	model textinput.Model
	value *string

	// options is the list of options for cycling through.
	options []string

	// If scrolling through, optionIdx is the current index in options,
	// or -1 if the user has entered a custom value.
	optionIdx int
}

var _ Field = (*Input)(nil)

// NewInput builds a new input field.
func NewInput() *Input {
	m := textinput.New()
	m.Prompt = "" // we have our own prompt
	m.Cursor.SetMode(cursor.CursorStatic)
	return &Input{
		KeyMap:    DefaultInputKeyMap,
		Style:     DefaultInputStyle,
		model:     m,
		value:     new(string),
		optionIdx: -1,
	}
}

// WithValue sets the destination for the input field.
// If the value is non-empty, it will be used as the initial value.
func (i *Input) WithValue(value *string) *Input {
	i.value = value
	return i
}

// WithOptions sets the list of options
// that can be cycled through with arrow keys.
// The options are cycled-through in order with wrap-around.
// If the initial value from WithValue matches one of the options,
// that option will be selected initially.
// Otherwise, the input is considered a custom value,
// and the first option will be selected when the user first presses down.
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

const (
	_scrollUpSymbol   = "▲"
	_scrollDownSymbol = "▼"
)

// Description returns the description of the input field.
// If there are options to choose from,
// the description includes markers for scrolling.
func (i *Input) Description() string {
	if len(i.options) <= 1 {
		return i.desc
	}

	var desc strings.Builder
	desc.WriteString(i.desc)
	if len(i.desc) > 0 {
		desc.WriteString(" ")
	}
	desc.WriteString("(")
	if i.optionIdx == -1 || i.optionIdx > 0 {
		desc.WriteString(_scrollUpSymbol)
	}
	if i.optionIdx == -1 || i.optionIdx < len(i.options)-1 {
		desc.WriteString(_scrollDownSymbol)
	}
	desc.WriteString(" for other options)")
	return desc.String()
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
				i.optionIdx = idx
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

		case key.Matches(keyMsg, i.KeyMap.Up) && len(i.options) > 0:
			i.optionIdx--
			if i.optionIdx < 0 {
				i.optionIdx = len(i.options) - 1
			}

			i.model.SetValue(i.options[i.optionIdx])
			i.model.CursorEnd()
			*i.value = i.options[i.optionIdx]
			return nil

		case key.Matches(keyMsg, i.KeyMap.Down) && len(i.options) > 0:
			i.optionIdx++
			if i.optionIdx >= len(i.options) {
				i.optionIdx = 0
			}

			i.model.SetValue(i.options[i.optionIdx])
			i.model.CursorEnd()
			*i.value = i.options[i.optionIdx]
			return nil
		}
	}

	var cmd tea.Cmd
	i.model, cmd = i.model.Update(msg)
	newValue := i.model.Value()

	// If the user manually edited the text, mark as custom value
	// so that next up/down starts from the first option.
	if i.optionIdx != -1 && newValue != i.options[i.optionIdx] {
		i.optionIdx = -1
	}

	*i.value = newValue
	return cmd
}

// Render renders the input field.
func (i *Input) Render(w Writer) {
	w.WriteString(i.model.View())
}
