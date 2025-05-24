package log

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"testing/slogtest"
	"time"

	"github.com/stretchr/testify/require"
)

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
