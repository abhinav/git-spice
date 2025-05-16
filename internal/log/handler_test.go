package log

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/slogtest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogHandler_Enabled(t *testing.T) {
	tests := []struct {
		name     string
		leveler  slog.Leveler
		enabled  []Level
		disabled []Level
	}{
		{
			name:    "trace",
			leveler: LevelTrace,
			enabled: []Level{LevelTrace, LevelDebug, LevelInfo},
		},
		{
			name:     "debug",
			leveler:  LevelDebug,
			enabled:  []Level{LevelDebug, LevelInfo},
			disabled: []Level{LevelTrace},
		},
		{
			name:     "info",
			leveler:  LevelInfo,
			enabled:  []Level{LevelInfo, LevelWarn},
			disabled: []Level{LevelDebug, LevelTrace},
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

func TestLogHandler_slogtest(t *testing.T) {
	var (
		buffer strings.Builder
		saver  saveTimeHandler
	)
	slogtest.Run(t, func(*testing.T) slog.Handler {
		buffer.Reset()

		saver.Handler = newLogHandler(&buffer, slog.LevelDebug, PlainStyle())
		return &saver
	}, func(t *testing.T) map[string]any {
		attrs := make(map[string]any)
		if !saver.saved.IsZero() {
			attrs[slog.TimeKey] = saver.saved
		}

		line := strings.TrimSpace(buffer.String())
		lvlstr, line, ok := strings.Cut(line, lvlDelim)
		require.True(t, ok, "missing level delimiter: %q", buffer.String())

		switch lvlstr {
		case "DBG":
			attrs[slog.LevelKey] = slog.LevelDebug
		case "INF":
			attrs[slog.LevelKey] = slog.LevelInfo
		case "WRN":
			attrs[slog.LevelKey] = slog.LevelWarn
		case "ERR":
			attrs[slog.LevelKey] = slog.LevelError
		default:
			t.Fatalf("unknown level: %q", lvlstr)
		}

		attrs[slog.MessageKey], line, _ = strings.Cut(line, msgAttrDelim)

		for pair := range strings.SplitSeq(line, attrDelim) {
			if pair == "" {
				continue
			}
			key, value, ok := strings.Cut(pair, "=")
			require.True(t, ok, "missing attribute delimiter: %q", pair)

			curAttrs := attrs
			for len(key) > 0 {
				groupKey, valKey, ok := strings.Cut(key, groupDelim)
				if !ok {
					// No more groups.
					curAttrs[key] = value
					break
				}

				groupAttrs, ok := curAttrs[groupKey].(map[string]any)
				if !ok {
					groupAttrs = make(map[string]any)
					curAttrs[groupKey] = groupAttrs
				}
				curAttrs = groupAttrs
				key = valKey
			}
		}

		t.Logf("buffer: %q", buffer.String())
		t.Logf("attrs: %q", attrs)
		return attrs
	})
}

type saveTimeHandler struct {
	slog.Handler

	saved time.Time
}

func (h *saveTimeHandler) Handle(ctx context.Context, rec slog.Record) error {
	h.saved = rec.Time
	return h.Handler.Handle(ctx, rec)
}
