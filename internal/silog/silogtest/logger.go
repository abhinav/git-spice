// Package silogtest provides a logger for testing.
package silogtest

import (
	"io"

	"go.abhg.dev/gs/internal/silog"
)

// T is a subset of the testing.TB interface.
type T interface {
	Helper()
	Output() io.Writer
}

// New creates a new logger that writes to the given testing.TB.
func New(t T) *silog.Logger {
	t.Helper()

	return silog.New(t.Output(), &silog.Options{
		Level: silog.LevelDebug,
	})
}
