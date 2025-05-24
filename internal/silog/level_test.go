package silog_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/silog"
)

func TestLevel_String(t *testing.T) {
	tests := []struct {
		level    silog.Level
		expected string
	}{
		{silog.LevelDebug, "debug"},
		{silog.LevelInfo, "info"},
		{silog.LevelWarn, "warn"},
		{silog.LevelError, "error"},
		{silog.LevelFatal, "fatal"},
		{silog.Level(100), "ERROR+92"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.level.String())
		})
	}
}

func TestByLevel_Get(t *testing.T) {
	byLevel := silog.ByLevel[string]{
		Debug: "debug",
		Info:  "info",
		Warn:  "warn",
		Error: "error",
		Fatal: "fatal",
	}

	tests := []struct {
		level silog.Level
		want  string
	}{
		{silog.LevelDebug, "debug"},
		{silog.LevelInfo, "info"},
		{silog.LevelWarn, "warn"},
		{silog.LevelError, "error"},
		{silog.LevelFatal, "fatal"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := byLevel.Get(tt.level)
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("unknown", func(t *testing.T) {
		assert.Panics(t, func() {
			byLevel.Get(silog.Level(100))
		})
	})
}
