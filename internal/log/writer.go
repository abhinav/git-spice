package log

import (
	"io"

	"go.abhg.dev/io/ioutil"
)

// Writer builds and returns an io.Writer that
// writes messages to the given logger.
// If the logger is nil, a no-op writer is returned.
//
// If prefix is non-empty, it is prepended to each message.
// The done function must be called when the writer is no longer needed.
// It will flush any buffered text to the logger.
//
// The returned writer is not thread-safe.
func Writer(log *Logger, lvl Level) (w io.Writer, done func()) {
	if log == nil {
		return io.Discard, func() {}
	}

	w, flush := ioutil.PrintfWriter(func(msg string, args ...any) {
		log.Logf(lvl, msg, args...)
	}, "")
	return w, flush
}
