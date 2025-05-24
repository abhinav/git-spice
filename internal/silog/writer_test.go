package silog_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/silog"
)

func TestLogWriter(t *testing.T) {
	var buf bytes.Buffer
	logger := silog.New(&buf, nil)
	writer, done := silog.Writer(logger, silog.LevelInfo)

	_, err := fmt.Fprint(writer, "hello world")
	require.NoError(t, err)
	done()

	assert.Equal(t, "INF hello world\n", buf.String())
}

func TestLogWriter_nil(t *testing.T) {
	writer, done := silog.Writer(nil, silog.LevelInfo)

	_, err := fmt.Fprint(writer, "hello world")
	require.NoError(t, err)
	done()
}
