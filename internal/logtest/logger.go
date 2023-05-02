// Package logtest provides a log.Logger for testing.
package logtest

import (
	"log"
	"testing"

	"go.abhg.dev/git-stack/internal/iox"
)

// New builds a logger that writes messages
// to the given testing.TB.
func New(t testing.TB) *log.Logger {
	return log.New(iox.TestWriter(t, ""), "", 0)
}
