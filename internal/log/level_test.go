package log_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/log"
)

func TestLevel_String(t *testing.T) {
	tests := []struct {
		level    log.Level
		expected string
	}{
		{log.LevelTrace, "trace"},
		{log.LevelDebug, "debug"},
		{log.LevelInfo, "info"},
		{log.LevelWarn, "warn"},
		{log.LevelError, "error"},
		{log.LevelFatal, "fatal"},
		{log.Level(100), "ERROR+92"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.level.String())
		})
	}
}

func TestByLevel_Get(t *testing.T) {
	byLevel := log.ByLevel[string]{
		Trace: "trace",
		Debug: "debug",
		Info:  "info",
		Warn:  "warn",
		Error: "error",
		Fatal: "fatal",
	}

	tests := []struct {
		level log.Level
		want  string
	}{
		{log.LevelTrace, "trace"},
		{log.LevelDebug, "debug"},
		{log.LevelInfo, "info"},
		{log.LevelWarn, "warn"},
		{log.LevelError, "error"},
		{log.LevelFatal, "fatal"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := byLevel.Get(tt.level)
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("unknown", func(t *testing.T) {
		assert.Panics(t, func() {
			byLevel.Get(log.Level(100))
		})
	})
}
