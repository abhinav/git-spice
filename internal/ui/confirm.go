package ui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ConfirmKeyMap defines the key bindings for [Confirm].
type ConfirmKeyMap struct {
	Yes    key.Binding
	No     key.Binding
	Accept key.Binding
}

// DefaultConfirmKeyMap is the default key map for a [Confirm] field.
var DefaultConfirmKeyMap = ConfirmKeyMap{
	Yes: key.NewBinding(
		key.WithKeys("y", "Y"),
		key.WithHelp("y", "yes"),
	),
	No: key.NewBinding(
		key.WithKeys("n", "N"),
		key.WithHelp("n", "no"),
	),
	Accept: key.NewBinding(
		key.WithKeys("enter", "tab"),
		key.WithHelp("enter/tab", "accept"),
	),
}

// ConfirmStyle configures the appearance of a [Confirm] field.
type ConfirmStyle struct {
	Key lipgloss.Style // how to highlight keys
}

// DefaultConfirmStyle is the default style for a [Confirm] field.
var DefaultConfirmStyle = ConfirmStyle{
	Key: lipgloss.NewStyle().Foreground(Magenta),
}

// Confirm is a boolean confirmation field that takes a yes or no answer.
type Confirm struct {
	KeyMap ConfirmKeyMap
	Style  ConfirmStyle

	title string
	desc  string
	value *bool
}

var _ Field = (*Confirm)(nil)

// NewConfirm builds a new confirm field that prompts the user
// with a yes or no question.
func NewConfirm() *Confirm {
	return &Confirm{
		KeyMap: DefaultConfirmKeyMap,
		Style:  DefaultConfirmStyle,
		value:  new(bool),
	}
}

// Err reports any errors in the confirm field.
func (c *Confirm) Err() error {
	return nil
}

// WithValue sets the destination for the confirm field.
// The result of the field will be written to the given boolean pointer.
// The pointer's current value will be used as the default.
func (c *Confirm) WithValue(value *bool) *Confirm {
	c.value = value
	return c
}

// Value returns the current value of the confirm field.
func (c *Confirm) Value() bool {
	return *c.value
}

// WithTitle sets the title for the confirm field.
func (c *Confirm) WithTitle(title string) *Confirm {
	c.title = title
	return c
}

// Title returns the title for the confirm field.
func (c *Confirm) Title() string {
	return c.title
}

// WithDescription sets the desc for the confirm field.
func (c *Confirm) WithDescription(desc string) *Confirm {
	c.desc = desc
	return c
}

// Description returns the description for the confirm field.
func (c *Confirm) Description() string {
	return c.desc
}

// Update handles a bubbletea event.
func (c *Confirm) Update(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd

	if msg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(msg, c.KeyMap.Yes):
			*c.value = true
			cmds = append(cmds, tea.Quit)

		case key.Matches(msg, c.KeyMap.No):
			*c.value = false
			cmds = append(cmds, tea.Quit)

		case key.Matches(msg, c.KeyMap.Accept):
			cmds = append(cmds, AcceptField)
		}
	}

	return tea.Batch(cmds...)
}

// Render renders the confirm field to the given writer.
func (c *Confirm) Render(w Writer) {
	w.WriteString("[")
	if *c.value {
		w.WriteString(c.Style.Key.Render("Y"))
		w.WriteString("/")
		w.WriteString(c.Style.Key.Render("n"))
	} else {
		w.WriteString(c.Style.Key.Render("y"))
		w.WriteString("/")
		w.WriteString(c.Style.Key.Render("N"))
	}
	w.WriteString("]")
}
