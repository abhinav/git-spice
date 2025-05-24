package silog_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/silog"
)

func TestDefaultStyle(t *testing.T) {
	assertHasValue := func(t *testing.T, style lipgloss.Style, msgArgs ...any) {
		t.Helper()

		assert.NotEmpty(t, strings.TrimSpace(style.String()), msgArgs...)
	}

	style := silog.DefaultStyle()

	assertHasValue(t, style.KeyValueDelimiter, "KeyValueDelimiter")
	assertHasValue(t, style.MultilinePrefix, "MultilinePrefix")
	assertHasValue(t, style.PrefixDelimiter, "PrefixDelimiter")

	for _, lvl := range silog.Levels {
		t.Run(lvl.String(), func(t *testing.T) {
			assertHasValue(t, style.LevelLabels.Get(lvl), "LevelLabels.Get(%s)", lvl)
		})
	}
}
