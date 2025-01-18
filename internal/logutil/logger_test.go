package logutil

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogWriter(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf)
	writer, done := Writer(logger, log.InfoLevel)

	_, err := fmt.Fprint(writer, "hello world")
	require.NoError(t, err)
	done()

	assert.Equal(t, "INFO hello world\n", buf.String())
}

func TestLogWriter_nil(t *testing.T) {
	writer, done := Writer(nil, log.InfoLevel)

	_, err := fmt.Fprint(writer, "hello world")
	require.NoError(t, err)
	done()
}

func TestTestLogger(t *testing.T) {
	var stub testOutputStub
	logger := TestLogger(&stub)

	logger.Infof("Hello, %s!", "world")
	logger.Error("Sadness", "err", errors.New("oh no"))

	assert.Equal(t, []string{
		"INFO Hello, world!",
		`ERRO Sadness err="oh no"`,
	}, stub.logs)
}

type testOutputStub struct {
	logs    []string
	cleanup func()
}

func (t *testOutputStub) Logf(format string, args ...any) {
	t.logs = append(t.logs, fmt.Sprintf(format, args...))
}

func (t *testOutputStub) Cleanup(f func()) {
	old := t.cleanup
	t.cleanup = func() {
		f()
		if old != nil {
			old()
		}
	}
}
