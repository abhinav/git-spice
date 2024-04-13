package logtest

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLogger(t *testing.T) {
	var stub testOutputStub
	logger := New(&stub)

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
