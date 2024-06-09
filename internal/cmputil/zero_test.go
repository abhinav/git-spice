package cmputil_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/cmputil"
)

func TestZero(t *testing.T) {
	assert.False(t, cmputil.Zero(1))
	assert.True(t, cmputil.Zero(0))
}
