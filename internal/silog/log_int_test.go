package silog

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLogger_nilSafeFatal(t *testing.T) {
	defer func(old func(int)) {
		_osExit = old
	}(_osExit)

	var exited bool
	_osExit = func(int) {
		exited = true
	}

	var logger *Logger
	logger.Fatal("foo")
	assert.True(t, exited)

	exited = false
	logger.Fatalf("foo %s", "bar")
	assert.True(t, exited)
}
