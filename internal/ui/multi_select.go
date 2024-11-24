package ui

import (
	"slices"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"go.abhg.dev/gs/internal/must"
)

// MultiSelectKeyMap defines the key bindings for [MultiSelect].
type MultiSelectKeyMap struct {
	Up, Down key.Binding
	Toggle   key.Binding
	Accept   key.Binding
}

// DefaultMultiSelectKeyMap is the default key map for a [MultiSelect].
var DefaultMultiSelectKeyMap = MultiSelectKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("up/k", "move up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("down/j", "move down"),
	),
	Toggle: key.NewBinding(
		key.WithKeys(" ", "right"),
		key.WithHelp("space", "toggle split"),
	),
	Accept: key.NewBinding(
		key.WithKeys("enter", "tab"),
		key.WithHelp("enter/tab", "accept"),
	),
}

// MultiSelectStyle defines the styles for [MultiSelect].
type MultiSelectStyle struct {
	// Cursor is the string to use for the cursor.
	Cursor lipgloss.Style

	// Done is the string for the Done button.
	Done lipgloss.Style

	// ScrollUp is the string for the scroll up marker.
	ScrollUp lipgloss.Style

	// ScrollDown is the string for the scroll down marker.
	ScrollDown lipgloss.Style
}

// DefaultMultiSelectStyle is the default style for a [MultiSelect].
var DefaultMultiSelectStyle = MultiSelectStyle{
	Cursor:     NewStyle().Foreground(Yellow).Bold(true).SetString("▶"),
	Done:       NewStyle().Foreground(Green).SetString("Done"),
	ScrollUp:   NewStyle().Foreground(Gray).SetString("▲▲▲"),
	ScrollDown: NewStyle().Foreground(Gray).SetString("▼▼▼"),
}

// MultiSelect is a prompt that allows selecting one or more options.
type MultiSelect[T any] struct {
	KeyMap MultiSelectKeyMap
	Style  MultiSelectStyle

	title string
	desc  string

	renderOption func(Writer, int, MultiSelectOption[T])
	options      []MultiSelectOption[T]

	cursor  mutliSelectCursor
	visible int // number of visible options
	offset  int // offset of the first visible option

	accepted bool
}

var _ Field = (*MultiSelect[int])(nil)

// NewMultiSelect constructs a new multi-select field.
func NewMultiSelect[T any](render func(Writer, int, MultiSelectOption[T])) *MultiSelect[T] {
	return &MultiSelect[T]{
		renderOption: render,
		KeyMap:       DefaultMultiSelectKeyMap,
		Style:        DefaultMultiSelectStyle,
	}
}

// Selected returns the indexes of the selected options.
func (s *MultiSelect[T]) Selected() []int {
	var selected []int
	for i, option := range s.options {
		if option.Selected {
			selected = append(selected, i)
		}
	}
	return selected
}

// Value returns a slice of the selected values.
func (s *MultiSelect[T]) Value() []T {
	items := make([]T, 0, len(s.options))
	for _, option := range s.options {
		if option.Selected {
			items = append(items, option.Value)
		}
	}
	return items
}

// UnmarshalValue unmarshals the result of the field from the given function.
// The input source must be a list of indexes, not []T.
func (s *MultiSelect[T]) UnmarshalValue(unmarshal func(any) error) error {
	var selected []int
	if err := unmarshal(&selected); err != nil {
		return err
	}

	for i := range s.options {
		s.options[i].Selected = slices.Contains(selected, i)
	}

	return nil
}

// MultiSelectOption is an option for a multi-select field.
type MultiSelectOption[T any] struct {
	// Value of the option.
	Value T

	// Selected indicates whether the option is already selected.
	Selected bool

	// Skip indicates whether the option should be skipped.
	// Skipped options are not selectable
	// and will never be included in the result.
	Skip bool
}

// WithOptions sets the options for the multi-select field.
// Options will be presented in the order they are provided.
// The existing options, if any, will be replaced.
func (s *MultiSelect[T]) WithOptions(opts ...MultiSelectOption[T]) *MultiSelect[T] {
	s.options = slices.Clone(opts)
	return s
}

// WithTitle sets the title of the multi-select field.
func (s *MultiSelect[T]) WithTitle(title string) *MultiSelect[T] {
	s.title = title
	return s
}

// Title returns the title of the multi-select field.
func (s *MultiSelect[T]) Title() string {
	return s.title
}

// WithDescription sets the description of the multi-select field.
func (s *MultiSelect[T]) WithDescription(desc string) *MultiSelect[T] {
	s.desc = desc
	return s
}

// Description returns the description of the multi-select field.
func (s *MultiSelect[T]) Description() string {
	return s.desc
}

// Err returns nil.
func (s *MultiSelect[T]) Err() error {
	return nil
}

// Init initializes the multi-select field.
func (s *MultiSelect[T]) Init() tea.Cmd {
	s.cursor = mutliSelectCursor{len: len(s.options)}
	return nil
}

// Update updates the multi-select field.
func (s *MultiSelect[T]) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.visible = msg.Height - 5 // two scroll markers + Done

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, s.KeyMap.Up):
			s.moveCursor(false /* backwards */)

		case key.Matches(msg, s.KeyMap.Down):
			s.moveCursor(true /* forwards */)

		case key.Matches(msg, s.KeyMap.Accept):
			if s.cursor.done() {
				s.accepted = true
				return tea.Quit
			}

			// Convenience:
			// Allow Enter to toggle the focused option.
			fallthrough

		case key.Matches(msg, s.KeyMap.Toggle):
			if s.cursor.done() {
				break // no-op
			}

			must.NotBef(s.options[s.cursor.idx].Skip,
				"skipped option should not be toggled: %d", s.cursor.idx)
			s.options[s.cursor.idx].Selected = !s.options[s.cursor.idx].Selected
			s.moveCursor(true /* forwards */)
		}
	}

	return nil
}

// multiSelectCursor has one of two states:
// it's focused on an option, or it's focused on the Done button.
// When idx == len, the cursor is focused on the Done button.
type mutliSelectCursor struct{ idx, len int }

func (c mutliSelectCursor) add(delta int) mutliSelectCursor {
	c.idx += delta
	if c.idx < 0 {
		c.idx = c.len
	}
	if c.idx > c.len {
		c.idx = 0
	}
	return c
}

func (c mutliSelectCursor) done() bool {
	return c.idx == c.len
}

func (s *MultiSelect[T]) moveCursor(forwards bool) {
	delta := 1
	if !forwards {
		delta = -1
	}

	cursor := s.cursor.add(delta) // will always be in bounds or == len
	for !cursor.done() && s.options[cursor.idx].Skip {
		cursor = cursor.add(delta)
	}

	s.cursor = cursor

	// Adjust pagination if necessary.
	itemIdx := s.cursor.idx
	if s.cursor.done() {
		itemIdx = s.cursor.len - 1
	}
	if itemIdx < s.offset {
		s.offset = itemIdx
	}
	if itemIdx >= s.offset+s.visible {
		s.offset = itemIdx - s.visible + 1
	}
}

// Render renders the multi-select field.
func (s *MultiSelect[T]) Render(w Writer) {
	options := s.options
	var offset int

	// Need a scrollbar above the list.
	if s.visible < len(options) && !s.accepted {
		options = options[s.offset : s.offset+s.visible]
		offset = s.offset

		w.WriteString("\n")
		if s.offset > 0 {
			w.WriteString(s.Style.ScrollUp.String())
		}
	}

	for offsetIdx, option := range options {
		idx := offset + offsetIdx

		w.WriteString("\n")
		if idx == s.cursor.idx {
			w.WriteString(s.Style.Cursor.String())
		} else {
			w.WriteString(" ")
		}

		w.WriteString(" ")

		s.renderOption(w, idx, option)
	}

	// Need a scrollbar below the list.
	if s.visible < len(s.options) && !s.accepted {
		w.WriteString("\n")
		if s.offset+s.visible < len(s.options)-1 {
			w.WriteString(s.Style.ScrollDown.String())
		}
	}

	// Done button.
	if !s.accepted {
		w.WriteString("\n")
		if s.cursor.done() {
			w.WriteString(s.Style.Cursor.String())
		} else {
			w.WriteString(" ")
		}
		w.WriteString(" ")
		w.WriteString(s.Style.Done.String())
	}
}
