package widget

import (
	"fmt"
	"slices"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"go.abhg.dev/gs/internal/ui"
)

// FileSelectStyle defines the styles for [FileSelect].
type FileSelectStyle struct {
	// Checkbox is the style for an unselected checkbox.
	Checkbox lipgloss.Style

	// CheckboxSelected is the style for a selected checkbox.
	CheckboxSelected lipgloss.Style

	// Status is the style for the file status indicator.
	Status lipgloss.Style

	// Path is the style for the file path.
	Path lipgloss.Style

	// OldPath is the style for the old path in renames.
	OldPath lipgloss.Style

	// Arrow is the style for the rename arrow.
	Arrow lipgloss.Style
}

// DefaultFileSelectStyle is the default style for [FileSelect].
var DefaultFileSelectStyle = FileSelectStyle{
	Checkbox:         ui.NewStyle().SetString("[ ]"),
	CheckboxSelected: ui.NewStyle().Foreground(ui.Green).SetString("[x]"),
	Status:           ui.NewStyle().Foreground(ui.Yellow).Bold(true),
	Path:             ui.NewStyle(),
	OldPath:          ui.NewStyle().Foreground(ui.Gray),
	Arrow:            ui.NewStyle().Foreground(ui.Gray).SetString(" → "),
}

// FileSelectKeyMap defines the key bindings for [FileSelect].
type FileSelectKeyMap struct {
	Up, Down key.Binding
	Toggle   key.Binding
	Accept   key.Binding
}

// DefaultFileSelectKeyMap is the default key map for [FileSelect].
var DefaultFileSelectKeyMap = FileSelectKeyMap{
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
		key.WithHelp("space", "toggle selection"),
	),
	Accept: key.NewBinding(
		key.WithKeys("enter", "tab"),
		key.WithHelp("enter/tab", "accept"),
	),
}

// FileEntry represents a file change entry for selection.
type FileEntry struct {
	// Status is the file status code (A, M, D, R, etc.).
	Status string

	// Path is the file path (new path for renames).
	Path string

	// OldPath is the old path for renamed files.
	// Empty for non-rename statuses.
	OldPath string
}

// String returns a display string for the file entry.
func (e FileEntry) String() string {
	if e.OldPath != "" {
		return fmt.Sprintf("%s %s → %s", e.Status, e.OldPath, e.Path)
	}
	return fmt.Sprintf("%s %s", e.Status, e.Path)
}

// FileSelect is a widget that allows selecting files from a list.
type FileSelect struct {
	Style  FileSelectStyle
	KeyMap FileSelectKeyMap

	title string
	desc  string

	files    []FileEntry
	selected []bool

	cursor  int
	visible int // number of visible options
	offset  int // offset of the first visible option

	accepted bool
}

var _ ui.Field = (*FileSelect)(nil)

// NewFileSelect creates a new [FileSelect] widget.
func NewFileSelect() *FileSelect {
	return &FileSelect{
		Style:  DefaultFileSelectStyle,
		KeyMap: DefaultFileSelectKeyMap,
	}
}

// WithFiles sets the files to select from.
func (f *FileSelect) WithFiles(files []FileEntry) *FileSelect {
	f.files = files
	f.selected = make([]bool, len(files))
	return f
}

// WithTitle sets the title of the widget.
func (f *FileSelect) WithTitle(title string) *FileSelect {
	f.title = title
	return f
}

// Title returns the title of the widget.
func (f *FileSelect) Title() string {
	return f.title
}

// WithDescription sets the description of the widget.
func (f *FileSelect) WithDescription(desc string) *FileSelect {
	f.desc = desc
	return f
}

// Description returns the description of the widget.
func (f *FileSelect) Description() string {
	return f.desc
}

// Err returns nil.
func (f *FileSelect) Err() error {
	return nil
}

// Selected returns the indexes of selected files.
func (f *FileSelect) Selected() []int {
	var result []int
	for i, sel := range f.selected {
		if sel {
			result = append(result, i)
		}
	}
	return result
}

// SelectedFiles returns the selected file entries.
func (f *FileSelect) SelectedFiles() []FileEntry {
	var result []FileEntry
	for i, sel := range f.selected {
		if sel {
			result = append(result, f.files[i])
		}
	}
	return result
}

// UnmarshalValue unmarshals selected file indexes.
func (f *FileSelect) UnmarshalValue(unmarshal func(any) error) error {
	var selected []int
	if err := unmarshal(&selected); err != nil {
		return err
	}
	for i := range f.selected {
		f.selected[i] = slices.Contains(selected, i)
	}
	return nil
}

// Init initializes the widget.
func (f *FileSelect) Init() tea.Cmd {
	return nil
}

// Update handles input events.
func (f *FileSelect) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		f.visible = msg.Height - 4

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, f.KeyMap.Up):
			f.moveCursor(-1)

		case key.Matches(msg, f.KeyMap.Down):
			f.moveCursor(1)

		case key.Matches(msg, f.KeyMap.Toggle):
			if f.cursor < len(f.files) {
				f.selected[f.cursor] = !f.selected[f.cursor]
				f.moveCursor(1)
			}

		case key.Matches(msg, f.KeyMap.Accept):
			f.accepted = true
			return ui.AcceptField
		}
	}
	return nil
}

func (f *FileSelect) moveCursor(delta int) {
	f.cursor += delta
	if f.cursor < 0 {
		f.cursor = 0
	}
	if f.cursor >= len(f.files) {
		f.cursor = len(f.files) - 1
	}

	// Adjust pagination.
	if f.cursor < f.offset {
		f.offset = f.cursor
	}
	if f.visible > 0 && f.cursor >= f.offset+f.visible {
		f.offset = f.cursor - f.visible + 1
	}
}

// Render renders the widget.
func (f *FileSelect) Render(w ui.Writer) {
	files := f.files
	selected := f.selected
	var offset int

	// Pagination.
	if f.visible > 0 && f.visible < len(files) && !f.accepted {
		end := min(f.offset+f.visible, len(files))
		files = files[f.offset:end]
		selected = f.selected[f.offset:end]
		offset = f.offset

		w.WriteString("\n")
		if f.offset > 0 {
			w.WriteString(ui.NewStyle().Foreground(ui.Gray).Render("  ▲▲▲"))
		}
	}

	for i, file := range files {
		idx := offset + i
		w.WriteString("\n")

		// Cursor.
		if idx == f.cursor && !f.accepted {
			w.WriteString(ui.NewStyle().Foreground(ui.Yellow).Bold(true).Render("▶"))
		} else {
			w.WriteString(" ")
		}
		w.WriteString(" ")

		// Checkbox.
		if selected[i] {
			w.WriteString(f.Style.CheckboxSelected.String())
		} else {
			w.WriteString(f.Style.Checkbox.String())
		}
		w.WriteString(" ")

		// Status.
		w.WriteString(f.Style.Status.Render(file.Status))
		w.WriteString(" ")

		// Path (handle renames).
		if file.OldPath != "" {
			w.WriteString(f.Style.OldPath.Render(file.OldPath))
			w.WriteString(f.Style.Arrow.String())
		}
		w.WriteString(f.Style.Path.Render(file.Path))
	}

	// Scroll down indicator.
	if f.visible > 0 && f.visible < len(f.files) && !f.accepted {
		w.WriteString("\n")
		if f.offset+f.visible < len(f.files) {
			w.WriteString(ui.NewStyle().Foreground(ui.Gray).Render("  ▼▼▼"))
		}
	}
}
