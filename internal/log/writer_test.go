package log_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/log"
)

func TestLogWriter(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, nil)
	writer, done := log.Writer(logger, log.LevelInfo)

	_, err := fmt.Fprint(writer, "hello world")
	require.NoError(t, err)
	done()

	assert.Equal(t, "INF hello world\n", buf.String())
}

func TestLogWriter_nil(t *testing.T) {
	writer, done := log.Writer(nil, log.LevelInfo)

	_, err := fmt.Fprint(writer, "hello world")
	require.NoError(t, err)
	done()
}
