package ui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ListKeyMap defines key bindings for [List].
type ListKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Accept key.Binding
}

// DefaultListKeyMap specifies the default key bindings for [List].
var DefaultListKeyMap = ListKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("up/k", "go up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("down/j", "go down"),
	),
	Accept: key.NewBinding(
		key.WithKeys("enter", "tab"),
		key.WithHelp("enter/tab", "accept"),
	),
}

// ListStyle defines the styles for [List].
type ListStyle struct {
	Cursor lipgloss.Style

	ItemTitle         lipgloss.Style
	SelectedItemTitle lipgloss.Style
}

// DefaultListStyle is the default style for a [List].
var DefaultListStyle = ListStyle{
	Cursor:            NewStyle().Foreground(Yellow).Bold(true).SetString("▶"),
	ItemTitle:         NewStyle().Foreground(Gray),
	SelectedItemTitle: NewStyle().Foreground(Yellow),
}

// List is a prompt that allows selecting from a list of options.
// This is similar to [Select] but without the fuzzy filter.
// Each item in a List can have a title, description, and a value.
type List[T any] struct {
	KeyMap ListKeyMap
	Style  ListStyle

	title string
	desc  string
	items []ListItem[T]
	value *T

	width    int
	selected int
	accepted bool
}

var _ Field = (*List[int])(nil)

// ListItem is an item in a [List].
type ListItem[T any] struct {
	Title       string
	Description func(focused bool) string
	Value       T
}

// NewList creates a new [List] with default settings.
func NewList[T any]() *List[T] {
	return &List[T]{
		KeyMap: DefaultListKeyMap,
		Style:  DefaultListStyle,
		value:  new(T),
	}
}

// WithValue sets the destination pointer for the selected item's value.
// When the user selects an item, the value will be copied to the pointer.
func (l *List[T]) WithValue(value *T) *List[T] {
	l.value = value
	return l
}

// Value retrieevs the selected item's value.
func (l *List[T]) Value() *T {
	return l.value
}

// UnmarshalValue reads a value from the given unmarshal function.
// It expects an index of the selected item, or the title of the selected item.
func (l *List[T]) UnmarshalValue(unmarshal func(any) error) error {
	var title string
	if err := unmarshal(&title); err == nil {
		for i, item := range l.items {
			if item.Title == title {
				*l.value = l.items[i].Value
				return nil
			}
		}

		return fmt.Errorf("item with title %q not found", title)
	}

	var idx int
	if err := unmarshal(&idx); err != nil {
		return err
	}

	if idx < 0 || idx >= len(l.items) {
		return fmt.Errorf("index %d is out of bounds [0, %d]", idx, len(l.items)-1)
	}

	*l.value = l.items[idx].Value
	return nil
}

// WithTitle sets the title of the [List].
func (l *List[T]) WithTitle(title string) *List[T] {
	l.title = title
	return l
}

// Title retrieves the title of the [List].
func (l *List[T]) Title() string {
	return l.title
}

// WithDescription sets the description of the [List].
func (l *List[T]) WithDescription(desc string) *List[T] {
	l.desc = desc
	return l
}

// Description retrieves the description of the [List].
func (l *List[T]) Description() string {
	return l.desc
}

// WithItems fills the list with items.
// By default the first of these items will be selected.
func (l *List[T]) WithItems(items ...ListItem[T]) *List[T] {
	l.items = items
	return l
}

// WithSelected sets the index of the selected item.
func (l *List[T]) WithSelected(selected int) *List[T] {
	l.selected = selected
	return l
}

// With is a helper to pass in a list of customizations at once.
func (l *List[T]) With(f func(l *List[T])) *List[T] {
	f(l)
	return l
}

// Err returns nil.
func (l *List[T]) Err() error { return nil }

// Init initializes the [List].
func (l *List[T]) Init() tea.Cmd {
	return nil
}

// Update receives a message from bubbletea
// and updates the internal state of the list.
func (l *List[T]) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		l.width = msg.Width

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, l.KeyMap.Up):
			l.selected--
			if l.selected < 0 {
				l.selected = len(l.items) - 1
			}

		case key.Matches(msg, l.KeyMap.Down):
			l.selected++
			if l.selected >= len(l.items) {
				l.selected = 0
			}

		case key.Matches(msg, l.KeyMap.Accept):
			if l.selected >= 0 && l.selected < len(l.items) {
				*l.value = l.items[l.selected].Value
				l.accepted = true
				return AcceptField
			}
		}
	}

	return nil
}

// Render renders the list to the screen.
func (l *List[T]) Render(w Writer) {
	if l.accepted {
		w.WriteString(l.items[l.selected].Title)
		return
	}

	for i, item := range l.items {
		titleStyle := l.Style.ItemTitle
		cursor := "  "
		descStyle := lipgloss.NewStyle().Width(l.width - 4)
		if i == l.selected {
			cursor = l.Style.Cursor.String() + " "
			titleStyle = l.Style.SelectedItemTitle
		} else {
			descStyle = descStyle.Faint(true)
			w.WriteString(" ")
		}

		w.WriteString("\n")
		w.WriteString(lipgloss.JoinHorizontal(
			lipgloss.Top,
			cursor,
			lipgloss.JoinVertical(
				lipgloss.Left,
				titleStyle.Render(item.Title),
				descStyle.Render(item.Description(i == l.selected)),
			),
		))
	}
}
