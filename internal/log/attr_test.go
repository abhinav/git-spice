package log_test

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/log"
)

func TestNonZero(t *testing.T) {
	t.Run("Int", func(t *testing.T) {
		zero := log.NonZero("zero", 0)
		one := log.NonZero("one", 1)
		assert.True(t, zero.Equal(slog.Attr{}))
		assert.True(t, one.Equal(slog.Int("one", 1)))
	})

	t.Run("String", func(t *testing.T) {
		empty := log.NonZero("empty", "")
		one := log.NonZero("one", "1")
		assert.True(t, empty.Equal(slog.Attr{}))
		assert.True(t, one.Equal(slog.String("one", "1")))
	})

	t.Run("Bool", func(t *testing.T) {
		zero := log.NonZero("zero", false)
		one := log.NonZero("one", true)
		assert.True(t, zero.Equal(slog.Attr{}))
		assert.True(t, one.Equal(slog.Bool("one", true)))
	})
}
