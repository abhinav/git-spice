package ui

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"go.abhg.dev/gs/internal/execedit"
	"go.abhg.dev/gs/internal/osutil"
)

// OpenEditorKeyMap defines the key bindings for [OpenEditor].
type OpenEditorKeyMap struct {
	Edit   key.Binding
	Accept key.Binding
}

// DefaultOpenEditorKeyMap is the default key map for an [OpenEditor] field.
var DefaultOpenEditorKeyMap = OpenEditorKeyMap{
	Edit: key.NewBinding(
		key.WithKeys("e"),
		key.WithHelp("e", "open editor"),
	),
	Accept: key.NewBinding(
		key.WithKeys("enter", "tab"),
		key.WithHelp("enter/tab", "accept"),
	),
}

// OpenEditorStyle defines the display style for [OpenEditor].
type OpenEditorStyle struct {
	Key    lipgloss.Style // how to highlight keys
	Editor lipgloss.Style

	NoEditorMessage string
}

// DefaultOpenEditorStyle is the default style for an [OpenEditor] field.
var DefaultOpenEditorStyle = OpenEditorStyle{
	Key:             NewStyle().Foreground(Magenta),
	Editor:          NewStyle().Foreground(Green),
	NoEditorMessage: "please set an editor",
}

// Editor configures the editor to open.
type Editor struct {
	// Command is the editor command to run.
	//
	// This may be a shell command like:
	//
	//	FOO=bar gvim --nofork
	//
	// or a binary name like:
	//
	//	nvim
	//
	// For the former case, we'll use 'sh -c' to run the command.
	Command string

	// Ext is the extension to assign to the file
	// before opening the editor.
	//
	// Defaults to "md".
	Ext string
}

// DefaultEditor returns the default editor configuration.
func DefaultEditor() Editor {
	return Editor{
		Command: os.Getenv("EDITOR"),
		Ext:     "md",
	}
}

// OpenEditor is a dialog that asks the user to press a key
// to open an editor and write a message.
type OpenEditor struct {
	KeyMap OpenEditorKeyMap
	Style  OpenEditorStyle
	Editor Editor

	title string
	desc  string

	value *string
	err   error
}

var _ Field = (*OpenEditor)(nil)

// NewOpenEditor builds an [OpenEditor] field.
// It will prompt the user to open an editor and write a message,
// or accept the current value.
func NewOpenEditor(editor Editor) *OpenEditor {
	return &OpenEditor{
		KeyMap: DefaultOpenEditorKeyMap,
		Style:  DefaultOpenEditorStyle,
		Editor: editor,
		value:  new(string),
	}
}

// Err reports any errors encountered during the operation.
func (a *OpenEditor) Err() error {
	return a.err
}

// WithValue specifies the value to edit.
// The current value will be used as the initial content of the editor.
// The value will be updated when the editor is closed,
// or left unchanged if the user skips the editor.
func (a *OpenEditor) WithValue(value *string) *OpenEditor {
	a.value = value
	return a
}

// WithTitle sets the title for the field.
func (a *OpenEditor) WithTitle(title string) *OpenEditor {
	a.title = title
	return a
}

// Title returns the title for the field.
func (a *OpenEditor) Title() string {
	return a.title
}

// WithDescription sets the description for the field.
func (a *OpenEditor) WithDescription(desc string) *OpenEditor {
	a.desc = desc
	return a
}

// Description returns the description for the field.
func (a *OpenEditor) Description() string {
	return a.desc
}

// Init initializes the field.
func (a *OpenEditor) Init() tea.Cmd {
	return nil
}

type updateEditorValueMsg []byte

// Update receives a new event from bubbletea
// and updates the field's internal state.
func (a *OpenEditor) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case updateEditorValueMsg:
		*a.value = string(msg)

		// The field is accepted automatically after the editor is
		// closed.
		return AcceptField

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, a.KeyMap.Edit) && a.Editor.Command != "":
			ext := strings.TrimPrefix(a.Editor.Ext, ".")

			tmpFile, err := osutil.TempFilePath("", "*."+ext)
			if err != nil {
				a.err = fmt.Errorf("create temporary file: %w", err)
				return tea.Quit
			}

			if err := os.WriteFile(tmpFile, []byte(*a.value), 0o644); err != nil {
				a.err = errors.Join(
					fmt.Errorf("write to temporary file: %w", err),
					os.Remove(tmpFile),
				)
				return tea.Quit
			}

			cmd := execedit.Command(a.Editor.Command, tmpFile)
			return tea.ExecProcess(cmd, func(err error) tea.Msg {
				defer func() { _ = os.Remove(tmpFile) }()

				if err != nil {
					a.err = fmt.Errorf("run editor: %w", err)
					return tea.Quit
				}

				content, err := os.ReadFile(tmpFile)
				if err != nil {
					a.err = fmt.Errorf("read temporary file: %w", err)
					return tea.Quit
				}

				return updateEditorValueMsg(content)
			})

		case key.Matches(msg, a.KeyMap.Accept):
			return AcceptField

		case a.Editor.Command == "":
			a.err = errors.New(a.Style.NoEditorMessage)
			return tea.Quit
		}
	}

	return nil
}

// Render renders the field to the screen.
func (a *OpenEditor) Render(w Writer) {
	fmt.Fprintf(w, "Press [%v] to open %v or [%v] to skip",
		a.Style.Key.Render(a.KeyMap.Edit.Help().Key),
		a.Style.Editor.Render(a.Editor.Command),
		a.Style.Key.Render(a.KeyMap.Accept.Help().Key),
	)
}
