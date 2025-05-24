package silog

import (
	"io"

	"go.abhg.dev/io/ioutil"
)

// LeveledLogger is any logger that can log at a specific level.
type LeveledLogger interface {
	Log(lvl Level, msg string, kvs ...any)
}

// Writer builds and returns an io.Writer that
// writes messages to the given logger.
// If the logger is nil, a no-op writer is returned.
//
// The done function must be called when the writer is no longer needed.
// It will flush any buffered text to the logger.
//
// The returned writer is not thread-safe.
func Writer(log LeveledLogger, lvl Level) (w io.Writer, done func()) {
	if log == nil {
		return io.Discard, func() {}
	}

	w, flush := ioutil.LineWriter(func(bs []byte) {
		log.Log(lvl, string(bs))
	})
	return w, flush
}
