package ui

import (
	"bytes"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileView_Write(t *testing.T) {
	var buf bytes.Buffer
	view := NewFileView(&buf)

	_, err := view.Write([]byte(
		lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff0000")).
			Render("hello"),
	))
	require.NoError(t, err)

	assert.Equal(t, "hello", buf.String())
}

func TestFileView_Theme(t *testing.T) {
	view := NewFileView(&bytes.Buffer{})
	theme := view.Theme()

	assert.NotNil(t, theme.Yellow)
	assert.NotNil(t, theme.Red)
	assert.NotNil(t, theme.Green)
	assert.NotNil(t, theme.Plain)
	assert.NotNil(t, theme.Cyan)
	assert.NotNil(t, theme.Magenta)
	assert.NotNil(t, theme.Gray)
}

func TestFileView_Theme_override(t *testing.T) {
	t.Setenv("__GIT_SPICE_THEME", "light")

	view := NewFileView(&bytes.Buffer{})

	assert.Equal(t, DefaultThemeLight(), view.Theme())
}
