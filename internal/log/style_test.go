package log_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/log"
)

func TestDefaultStyle(t *testing.T) {
	assertHasValue := func(t *testing.T, style lipgloss.Style, msgArgs ...any) {
		t.Helper()

		assert.NotEmpty(t, strings.TrimSpace(style.String()), msgArgs...)
	}

	style := log.DefaultStyle()

	assertHasValue(t, style.KeyValueDelimiter, "KeyValueDelimiter")
	assertHasValue(t, style.MultilinePrefix, "MultilinePrefix")

	for _, lvl := range log.Levels {
		t.Run(lvl.String(), func(t *testing.T) {
			assertHasValue(t, style.LevelLabels.Get(lvl), "LevelLabels.Get(%s)", lvl)
		})
	}
}
