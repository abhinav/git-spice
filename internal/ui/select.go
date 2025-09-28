package ui

import (
	"cmp"
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SelectKeyMap defines the key bindings for [Select].
type SelectKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Accept key.Binding

	DeleteFilterChar key.Binding
}

// DefaultSelectKeyMap is the default key map for a [Select].
var DefaultSelectKeyMap = SelectKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "ctrl+k"),
		key.WithHelp("up", "go up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "ctrl+j"),
		key.WithHelp("down", "go down"),
	),
	Accept: key.NewBinding(
		key.WithKeys("enter", "tab"),
		key.WithHelp("enter/tab", "accept"),
	),
	DeleteFilterChar: key.NewBinding(
		key.WithKeys("backspace", "ctrl+h"),
		key.WithHelp("backspace", "delete filter character"),
	),
}

// SelectStyle defines the styles for [Select].
type SelectStyle struct {
	Selected  lipgloss.Style
	Highlight lipgloss.Style

	ScrollMarker lipgloss.Style
}

// DefaultSelectStyle is the default style for a [Select].
var DefaultSelectStyle = SelectStyle{
	Selected:     NewStyle().Foreground(Yellow),
	Highlight:    NewStyle().Foreground(Cyan),
	ScrollMarker: NewStyle().Foreground(Gray),
}

// Select is a prompt that allows selecting from a list of options
// using a fuzzy filter.
type Select[T any] struct {
	KeyMap SelectKeyMap
	Style  SelectStyle

	title string
	desc  string
	value *T

	options  []selectOption[T] // list of options
	filter   []rune            // filter to match options
	matched  []int             // indexes of matched options
	selected int               // index in matched of selected option

	visible int // number of visible options, 0 means all (immutable)
	offset  int // offset of the first visible option (mutable)

	accepted bool  // true after the field has been accepted
	err      error // error state
}

// SelectOption is a single option for a select field.
type SelectOption[T any] struct {
	Label string
	Value T

	// Number of empty lines before this option.
	PaddingAbove int
}

type selectOption[T any] struct {
	Label        string // label to show for this option
	Value        T      // value to set when selected
	Highlights   []int  // indexes of runes to highlight
	PaddingAbove int    // number of empty lines before this option
}

var _ Field = (*Select[int])(nil)

// NewSelect builds a new [Select] field.
func NewSelect[T any]() *Select[T] {
	return &Select[T]{
		KeyMap: DefaultSelectKeyMap,
		Style:  DefaultSelectStyle,
		value:  new(T),
	}
}

// WithValue sets the destination for the select field.
// The existing value, if any, will be selected by default.
func (s *Select[T]) WithValue(value *T) *Select[T] {
	s.value = value
	return s
}

// Value reports the current value of the select field.
func (s *Select[T]) Value() T {
	return *s.value
}

// UnmarshalValue unmarshals the value of the select field
// using the provided unmarshal function.
//
// It accepts one of the following types:
//
//   - bool: if the value is true, accept the field
//   - string: pick the option with a matching label
func (s *Select[T]) UnmarshalValue(unmarshal func(any) error) error {
	if ok := new(bool); unmarshal(ok) == nil && *ok {
		// Leave the field as is.
		return nil
	}

	var selectLabel string
	if err := unmarshal(&selectLabel); err != nil {
		return err
	}

	for _, opt := range s.options {
		if strings.TrimSpace(opt.Label) == strings.TrimSpace(selectLabel) {
			*s.value = opt.Value
			return nil
		}
	}

	return fmt.Errorf("no option with label: %v", selectLabel)
}

// With runs the given function with the select field.
func (s *Select[T]) With(f func(*Select[T])) *Select[T] {
	f(s)
	return s
}

// ComparableOptions creates a list of options from a list of comparable values
// and sets the selected option.
//
// The default string representation of the value is used as the label.
func ComparableOptions[T comparable](selected T, opts ...T) func(*Select[T]) {
	var selectedIdx int
	options := make([]SelectOption[T], len(opts))
	for i, v := range opts {
		if v == selected {
			selectedIdx = i
		}
		options[i] = SelectOption[T]{
			Label: fmt.Sprintf("%v", v),
			Value: v,
		}
	}

	return func(s *Select[T]) {
		s.WithOptions(options...)
		s.WithSelected(selectedIdx)
	}
}

// OptionalComparableOptions is like [ComparableOptions],
// but it allows for the user to select "none" as an option.
// nil represents the none option.
// noneLabel is the label to use for the none option, defaulting to "None".
// The none option is always presented last.
func OptionalComparableOptions[T comparable](noneLabel string, selected *T, opts ...T) func(*Select[*T]) {
	options := make([]SelectOption[*T], 0, len(opts)+1) // +1 for none option
	selectedIdx := len(opts) - 1                        // default to none option
	for idx, v := range opts {
		if selected != nil && v == *selected {
			selectedIdx = idx
		}
		label := fmt.Sprintf("%v", v)
		options = append(options, SelectOption[*T]{
			Label: label,
			Value: &v,
		})
	}

	options = append(options, SelectOption[*T]{
		Label:        cmp.Or(noneLabel, "None"),
		Value:        nil,
		PaddingAbove: 1,
	})

	return func(s *Select[*T]) {
		s.WithOptions(options...)
		s.WithSelected(selectedIdx)
	}
}

// WithOptions sets the available options for the select field.
// The options will be presented in the order they are provided.
// Existing options will be replaced.
func (s *Select[T]) WithOptions(opts ...SelectOption[T]) *Select[T] {
	options := make([]selectOption[T], len(opts))
	matched := make([]int, len(options))
	for i, v := range opts {
		options[i] = selectOption[T]{
			Label:        v.Label,
			Value:        v.Value,
			PaddingAbove: v.PaddingAbove,
		}
		matched[i] = i
	}

	s.options = options
	s.matched = matched
	return s
}

// WithSelected sets the selected option for the select field.
func (s *Select[T]) WithSelected(selected int) *Select[T] {
	s.selected = selected
	return s
}

// Title returns the title of the select field.
func (s *Select[T]) Title() string {
	return s.title
}

// WithTitle sets the title for the select field.
func (s *Select[T]) WithTitle(title string) *Select[T] {
	s.title = title
	return s
}

// Description returns the description of the select field.
func (s *Select[T]) Description() string {
	return s.desc
}

// WithDescription sets the description for the select field.
func (s *Select[T]) WithDescription(desc string) *Select[T] {
	s.desc = desc
	return s
}

// WithVisible sets the number of visible options in the select field.
// If unset, a default is picked based on the terminal height.
func (s *Select[T]) WithVisible(visible int) *Select[T] {
	s.visible = visible
	return s
}

// Err reports any errors in the select field.
func (s *Select[T]) Err() error {
	return s.err
}

// Init initializes the field.
func (s *Select[T]) Init() tea.Cmd {
	s.selected = max(0, min(s.selected, len(s.matched)-1))
	return nil
}

// Update receives messages from bubbletea.
func (s *Select[T]) Update(msg tea.Msg) tea.Cmd {
	var (
		cmds          []tea.Cmd
		filterChanged bool
	)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if s.visible == 0 {
			// Leave enough room for title, description, error,
			// and two scroll markers.
			s.visible = msg.Height - 5
		}

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, s.KeyMap.Up):
			s.selected--
			if s.selected < s.offset {
				s.offset = s.selected
			}
			if s.selected < 0 {
				s.selected = len(s.matched) - 1
				s.offset = s.selected - s.visible + 1
			}

		case key.Matches(msg, s.KeyMap.Down):
			s.selected++
			if s.selected >= s.offset+s.visible {
				s.offset = s.selected - s.visible + 1
			}

			if s.selected >= len(s.matched) {
				s.selected = 0
				s.offset = 0
			}

		case key.Matches(msg, s.KeyMap.Accept):
			if s.selected < len(s.matched) {
				*s.value = s.options[s.matched[s.selected]].Value
				cmds = append(cmds, AcceptField)
				s.accepted = true
			}

		case key.Matches(msg, s.KeyMap.DeleteFilterChar):
			if len(s.filter) > 0 {
				s.filter = s.filter[:len(s.filter)-1]
				filterChanged = true
			}

		case msg.Type == tea.KeyRunes:
			for _, r := range msg.Runes {
				s.filter = append(s.filter, unicode.ToLower(r))
			}
			filterChanged = true

		}
	}

	if filterChanged {
		s.updateSuggestions()
	}

	return tea.Batch(cmds...)
}

func (s *Select[T]) updateSuggestions() {
	s.err = nil

	var selected string
	if s.selected < len(s.matched) {
		selected = s.options[s.matched[s.selected]].Label
	}

	var hasSelected bool
	s.matched = s.matched[:0]
	for i, opt := range s.options {
		if s.matchOption(&opt) {
			if opt.Label == selected {
				s.selected = len(s.matched)
				hasSelected = true
			}
			s.matched = append(s.matched, i)
			s.options[i] = opt
		}
	}

	if !hasSelected {
		s.selected = 0
	}
}

func (s *Select[T]) matchOption(opt *selectOption[T]) bool {
	opt.Highlights = opt.Highlights[:0]
	if len(s.filter) == 0 {
		return true
	}

	filter := s.filter
	for idx, r := range strings.ToLower(opt.Label) {
		if len(filter) == 0 {
			// Filter exhausted. Matched.
			return true
		}

		if r == filter[0] {
			opt.Highlights = append(opt.Highlights, idx)
			filter = filter[1:]
		}
	}

	return len(filter) == 0 // if any bit of filter is left, it's a mismatch
}

// Render renders the select field.
func (s *Select[T]) Render(out Writer) {
	// If the field has been accepted, only render the label
	// for the selected option.
	if s.accepted {
		out.WriteString(s.options[s.matched[s.selected]].Label)
		return
	}

	if s.title != "" {
		// If there's a title, we're currently on the same line as the
		// title following the ": " separator.
		out.WriteString("\n")
	}

	if len(s.matched) == 0 {
		s.err = fmt.Errorf("no matches for: %v", string(s.filter))
		return
	}

	highlight := s.Style.Highlight

	matched := s.matched
	offset := 0
	if s.visible > 0 && len(matched) > s.visible {
		matched = matched[s.offset : s.offset+s.visible]
		offset = s.offset
	}

	if offset > 0 {
		fmt.Fprintf(out, "%s\n", s.Style.ScrollMarker.Render("  ▲▲▲"))
	} else {
		out.WriteString("\n")
	}

	for matchIdx, optionIdx := range matched {
		matchIdx += offset

		// If there's padding above this option, add it now.
		for range s.options[optionIdx].PaddingAbove {
			out.WriteString("\n")
		}

		style := NewStyle()
		if matchIdx == s.selected {
			style = s.Style.Selected
			out.WriteString("▶ ")
		} else {
			out.WriteString("  ")
		}

		// Highlight the matched runes.
		value := s.options[optionIdx].Label
		lastRuneIdx := 0
		for _, runeIdx := range s.options[optionIdx].Highlights {
			out.WriteString(style.Render(value[lastRuneIdx:runeIdx]))
			out.WriteString(highlight.Render(string(value[runeIdx])))
			lastRuneIdx = runeIdx + 1
		}
		out.WriteString(style.Render(value[lastRuneIdx:]))
		out.WriteString("\n")
	}

	if offset+s.visible < len(s.matched) {
		fmt.Fprintf(out, "%s\n", s.Style.ScrollMarker.Render("  ▼▼▼"))
	}
}
