package ui

import (
	"strings"

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
	DefaultValue    lipgloss.Style // Y/N
	NonDefaultValue lipgloss.Style // y/n
}

// DefaultConfirmStyle is the default style for a [Confirm] field.
var DefaultConfirmStyle = ConfirmStyle{
	DefaultValue:    lipgloss.NewStyle().Foreground(_magentaColor),
	NonDefaultValue: lipgloss.NewStyle().Foreground(_plainColor),
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

// NewConfirm builds a new confirm field that writes its result
// to the given boolean pointer.
//
// The initial value of the boolean pointer will be used as the default.
func NewConfirm(value *bool) *Confirm {
	return &Confirm{
		KeyMap: DefaultConfirmKeyMap,
		Style:  DefaultConfirmStyle,
		value:  value,
	}
}

// Err reports any errors in the confirm field.
func (c *Confirm) Err() error {
	return nil
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

// View renders the confirm field.
func (c *Confirm) View() string {
	var s strings.Builder
	s.WriteString("[")
	if *c.value {
		s.WriteString(c.Style.DefaultValue.Render("Y"))
		s.WriteString("/")
		s.WriteString(c.Style.NonDefaultValue.Render("n"))
	} else {
		s.WriteString(c.Style.NonDefaultValue.Render("y"))
		s.WriteString("/")
		s.WriteString(c.Style.DefaultValue.Render("N"))
	}
	s.WriteString("]")
	return s.String()
}
