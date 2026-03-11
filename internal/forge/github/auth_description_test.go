package github

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/ui"
)

func TestDescription_usesThemeForFocusedURLs(t *testing.T) {
	theme := ui.DefaultThemeDark()

	got := patDesc(theme, true)

	assert.Contains(t, got, "\x1b[")
	assert.Contains(t, ansi.Strip(got), "https://github.com/settings/tokens")
}
