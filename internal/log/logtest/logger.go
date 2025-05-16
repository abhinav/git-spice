// Package logtest provides a logger for testing.
package logtest

import (
	"go.abhg.dev/gs/internal/log"
	"go.abhg.dev/io/ioutil"
)

// New creates a new logger that writes to the given testing.TB.
func New(t ioutil.TestLogger) *log.Logger {
	return log.New(ioutil.TestLogWriter(t, ""), &log.Options{
		Level: log.LevelTrace,
	})
}
