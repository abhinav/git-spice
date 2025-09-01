package silogtest_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestTestLogger(t *testing.T) {
	var stub testOutputStub
	logger := silogtest.New(&stub)

	logger.Infof("Hello, %s!", "world")
	logger.Error("Sadness", "error", errors.New("oh no"))

	assert.Equal(t, []string{
		"INF Hello, world!",
		`ERR Sadness  error=oh no`,
		"",
	}, strings.Split(stub.output.String(), "\n"))
}

type testOutputStub struct {
	output bytes.Buffer
}

func (t *testOutputStub) Helper() {}

func (t *testOutputStub) Output() io.Writer {
	return &t.output
}
