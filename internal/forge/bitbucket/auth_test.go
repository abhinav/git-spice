package bitbucket

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/ui"
)

func TestAPITokenAuthDescription_usesThemeForFocusedURL(t *testing.T) {
	theme := ui.DefaultThemeDark()

	got := apiTokenAuthDescription(theme, true)

	assert.Contains(t, got, "\x1b[")
	assert.Contains(t, ansi.Strip(got), "https://bitbucket.org/account/settings/api-tokens/")
}
