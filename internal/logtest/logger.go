// Package logtest provides a log.Logger for testing.
package logtest

import (
	"testing"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/ioutil"
)

// New builds a logger that writes messages
// to the given testing.TB.
func New(t testing.TB) *log.Logger {
	return log.New(ioutil.TestLogWriter(t, ""))
}
