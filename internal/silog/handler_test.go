package silog

import (
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
)

func TestLogHandler_Enabled(t *testing.T) {
	tests := []struct {
		name     string
		leveler  slog.Leveler
		enabled  []Level
		disabled []Level
	}{
		{
			name:    "debug",
			leveler: LevelDebug,
			enabled: []Level{LevelDebug, LevelInfo},
		},
		{
			name:     "info",
			leveler:  LevelInfo,
			enabled:  []Level{LevelInfo, LevelWarn},
			disabled: []Level{LevelDebug},
		},
		{
			name:     "warn",
			leveler:  LevelWarn,
			enabled:  []Level{LevelWarn, LevelError},
			disabled: []Level{LevelDebug, LevelInfo},
		},
		{
			name:     "error",
			leveler:  LevelError,
			enabled:  []Level{LevelError, LevelFatal},
			disabled: []Level{LevelInfo, LevelWarn},
		},
		{
			name:     "fatal",
			leveler:  LevelFatal,
			enabled:  []Level{LevelFatal},
			disabled: []Level{LevelWarn, LevelError},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newLogHandler(io.Discard, tt.leveler, PlainStyle())
			for _, level := range tt.enabled {
				assert.True(t, h.Enabled(t.Context(), level.Level()), "level %s should be enabled", level)
			}
			for _, level := range tt.disabled {
				assert.False(t, h.Enabled(t.Context(), level.Level()), "level %s should be disabled", level)
			}
		})
	}
}

func TestLogHandler_withAttrsConcurrent(t *testing.T) {
	const (
		NumWorkers = 10
		NumWrites  = 100
	)

	var buffer strings.Builder
	log := slog.New(newLogHandler(&buffer, LevelDebug, PlainStyle()))

	var ready, done sync.WaitGroup
	ready.Add(NumWorkers)
	for range NumWorkers {
		done.Add(1)
		go func() {
			defer done.Done()

			ready.Done()
			ready.Wait()

			for i := range NumWrites {
				log.Info("message", "i", i)
			}
		}()
	}

	done.Wait()

	assert.Equal(t, NumWorkers*NumWrites, strings.Count(buffer.String(), "INF message"))
}

func TestLogHandler_multilineMessageStyling(t *testing.T) {
	// Force colored output even if the terminal doesn't support it.
	t.Setenv("CLICOLOR_FORCE", "1")
	defer lipgloss.SetColorProfile(lipgloss.ColorProfile())
	lipgloss.SetColorProfile(termenv.EnvColorProfile())

	style := PlainStyle()
	style.Messages.Info = lipgloss.NewStyle().Bold(true)

	var buffer strings.Builder
	log := slog.New(newLogHandler(&buffer, LevelDebug, style))

	log.Info("foo\nbar")

	assert.Equal(t,
		"INF \x1b[1mfoo\x1b[0m\n"+
			"INF \x1b[1mbar\x1b[0m\n",
		buffer.String())
}
