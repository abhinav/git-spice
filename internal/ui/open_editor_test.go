package ui

import (
	"bytes"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveOpenEditorStyle_nil(t *testing.T) {
	var buf bytes.Buffer
	field := NewOpenEditor(Editor{Command: "vim"})

	field.Render(&buf, DefaultThemeLight())

	assert.Contains(t, buf.String(), "Press [")
	assert.Contains(t, buf.String(), " to open ")
}

func TestOpenEditor_Render_exactStyle(t *testing.T) {
	var buf bytes.Buffer
	field := NewOpenEditor(Editor{Command: "vim"})
	field.Style = OpenEditorStyle{
		Key:             Style{},
		Editor:          Style{},
		NoEditorMessage: "",
	}

	field.Render(&buf, DefaultThemeLight())

	assert.Equal(t, "Press [e] to open vim or [enter/tab] to skip", buf.String())
}

func TestOpenEditor_Update_noEditorUsesExactStyle(t *testing.T) {
	field := NewOpenEditor(Editor{})
	field.Style = OpenEditorStyle{
		Key:             Style{},
		Editor:          Style{},
		NoEditorMessage: "",
	}

	cmd := field.Update(tea.KeyPressMsg{Text: "x"})

	require.NotNil(t, cmd)
	assert.Equal(t, tea.Quit(), cmd())
	require.Error(t, field.Err())
	assert.Equal(t, "", field.Err().Error())
}
