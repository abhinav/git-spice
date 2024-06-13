package ui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Renderer is a lipgloss renderer that writes to stderr.
//
// We print prompts to stderr, so that's what we should use
// to check for colorization of lipgloss.
var Renderer = lipgloss.NewRenderer(os.Stderr)

func init() {
	lipgloss.SetDefaultRenderer(Renderer)
}

// NewStyle returns a new lipgloss style based on our default renderer.
func NewStyle() lipgloss.Style {
	return Renderer.NewStyle()
}
