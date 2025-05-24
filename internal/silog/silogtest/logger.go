// Package silogtest provides a logger for testing.
package silogtest

import (
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/io/ioutil"
)

// New creates a new logger that writes to the given testing.TB.
func New(t ioutil.TestLogger) *silog.Logger {
	return silog.New(ioutil.TestLogWriter(t, ""), &silog.Options{
		Level: silog.LevelDebug,
	})
}
