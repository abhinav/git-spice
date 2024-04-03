// Package logtest provides a log.Logger for testing.
package logtest

import (
	"testing"

	"github.com/rs/zerolog"
)

// New builds a logger that writes messages
// to the given testing.TB.
func New(t testing.TB) *zerolog.Logger {
	log := zerolog.New(
		zerolog.NewConsoleWriter(
			zerolog.ConsoleTestWriter(t),
		),
	).Level(zerolog.DebugLevel)
	return &log
}
