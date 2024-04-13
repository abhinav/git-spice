// Package logtest provides a log.Logger for testing.
package logtest

import (
	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/ioutil"
)

// New builds a logger that writes messages
// to the given testing.TB.
func New(t ioutil.TestOutput) *log.Logger {
	return log.New(ioutil.TestOutputWriter(t, ""))
}
