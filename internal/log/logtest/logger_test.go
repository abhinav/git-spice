package logtest_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/log/logtest"
)

func TestTestLogger(t *testing.T) {
	var stub testOutputStub
	logger := logtest.New(&stub)

	logger.Infof("Hello, %s!", "world")
	logger.Error("Sadness", "error", errors.New("oh no"))

	assert.Equal(t, []string{
		"INF Hello, world!",
		`ERR Sadness  error=oh no`,
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
