package ui

import (
	"fmt"
	"slices"
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
		key.WithKeys("up"),
		key.WithHelp("up", "go up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down"),
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
	Selected:     lipgloss.NewStyle().Foreground(Yellow),
	Highlight:    lipgloss.NewStyle().Foreground(Cyan),
	ScrollMarker: lipgloss.NewStyle().Foreground(Gray),
}

// Select is a prompt that allows selecting from a list of options
// using a fuzzy filter.
type Select struct {
	KeyMap SelectKeyMap
	Style  SelectStyle

	title string
	desc  string
	value *string

	options  []selectOption // list of options
	filter   []rune         // filter to match options
	matched  []int          // indexes of matched options
	selected int            // index in matched of selected option

	visible int // number of visible options, 0 means all (immutable)
	offset  int // offset of the first visible option (mutable)

	err error // error state
}

type selectOption struct {
	Value      string // value of the option
	Highlights []int  // indexes of runes to highlight
}

var _ Field = (*Select)(nil)

// NewSelect builds a new [Select] field.
func NewSelect() *Select {
	return &Select{
		KeyMap: DefaultSelectKeyMap,
		Style:  DefaultSelectStyle,
		value:  new(string),
	}
}

// WithValue sets the destination for the select field.
// The existing value, if any, will be selected by default.
func (s *Select) WithValue(value *string) *Select {
	s.value = value
	return s
}

// Value reports the current value of the select field.
func (s *Select) Value() string {
	return *s.value
}

// WithOptions sets the available options for the select field.
// The options will be presented in the order they are provided.
// Existing options will be replaced.
func (s *Select) WithOptions(opts ...string) *Select {
	options := make([]selectOption, len(opts))
	matched := make([]int, len(options))
	for i, v := range opts {
		options[i] = selectOption{
			Value: v,
		}
		matched[i] = i
	}

	s.options = options
	s.matched = matched
	return s
}

// Title returns the title of the select field.
func (s *Select) Title() string {
	return s.title
}

// WithTitle sets the title for the select field.
func (s *Select) WithTitle(title string) *Select {
	s.title = title
	return s
}

// Description returns the description of the select field.
func (s *Select) Description() string {
	return s.desc
}

// WithDescription sets the description for the select field.
func (s *Select) WithDescription(desc string) *Select {
	s.desc = desc
	return s
}

// WithVisible sets the number of visible options in the select field.
// If unset, a default is picked based on the terminal height.
func (s *Select) WithVisible(visible int) *Select {
	s.visible = visible
	return s
}

// Err reports any errors in the select field.
func (s *Select) Err() error {
	return s.err
}

// Init initializes the field.
func (s *Select) Init() tea.Cmd {
	idx := slices.IndexFunc(s.matched, func(optIdx int) bool {
		return s.options[optIdx].Value == *s.value
	})
	if idx != -1 {
		s.selected = idx
	}
	return nil
}

// Update receives messages from bubbletea.
func (s *Select) Update(msg tea.Msg) tea.Cmd {
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

func (s *Select) updateSuggestions() {
	s.err = nil

	var selected string
	if s.selected < len(s.matched) {
		selected = s.options[s.matched[s.selected]].Value
	}

	var hasSelected bool
	s.matched = s.matched[:0]
	for i, opt := range s.options {
		if s.matchOption(&opt) {
			if opt.Value == selected {
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

func (s *Select) matchOption(opt *selectOption) bool {
	opt.Highlights = opt.Highlights[:0]
	if len(s.filter) == 0 {
		return true
	}

	filter := s.filter
	for idx, r := range strings.ToLower(opt.Value) {
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
func (s *Select) Render(out Writer) {
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
		style := lipgloss.NewStyle()
		if matchIdx == s.selected {
			style = s.Style.Selected
			out.WriteString("▶ ")
		} else {
			out.WriteString("  ")
		}

		// Highlight the matched runes.
		value := s.options[optionIdx].Value
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
