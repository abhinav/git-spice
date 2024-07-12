package stub_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/stub"
)

func TestValue(t *testing.T) {
	v := 42
	restore := stub.Value(&v, 43)
	assert.Equal(t, 43, v)
	restore()
	assert.Equal(t, 42, v)
}

func TestFunc(t *testing.T) {
	fn := func() int { return 42 }

	restore := stub.Func(&fn, 43)
	assert.Equal(t, 43, fn())
	restore()
	assert.Equal(t, 42, fn())
}
