package must

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBef(t *testing.T) {
	assert.Panics(t, func() {
		Bef(false, "false")
	})

	assert.NotPanics(t, func() {
		Bef(true, "true")
	})
}

func TestNotBef(t *testing.T) {
	assert.Panics(t, func() {
		NotBef(true, "true")
	})

	assert.NotPanics(t, func() {
		NotBef(false, "false")
	})
}

func TestBeEqualf(t *testing.T) {
	assert.Panics(t, func() {
		BeEqualf(1, 2, "1 != 2")
	})

	assert.NotPanics(t, func() {
		BeEqualf(1, 1, "1 == 1")
	})
}

func TestBeEmptyMapf(t *testing.T) {
	assert.Panics(t, func() {
		BeEmptyMapf(map[int]int{1: 1}, "not empty")
	})

	assert.NotPanics(t, func() {
		BeEmptyMapf(map[int]int{}, "empty")
	})
}

func TestNotBeEqualf(t *testing.T) {
	assert.Panics(t, func() {
		NotBeEqualf(1, 1, "1 == 1")
	})

	assert.NotPanics(t, func() {
		NotBeEqualf(1, 2, "1 != 2")
	})
}

func TestNotBeBlankf(t *testing.T) {
	assert.Panics(t, func() {
		NotBeBlankf("", "empty")
	})

	assert.Panics(t, func() {
		NotBeBlankf(" ", "whitespace")
	})

	assert.NotPanics(t, func() {
		NotBeBlankf("a", "not blank")
	})
}

func TestNotBeEmptyf(t *testing.T) {
	assert.Panics(t, func() {
		NotBeEmptyf([]int{}, "empty")
	})

	assert.NotPanics(t, func() {
		NotBeEmptyf([]int{1}, "not empty")
	})
}

func TestNotBeNilf(t *testing.T) {
	assert.Panics(t, func() {
		NotBeNilf(nil, "nil")
	})

	assert.NotPanics(t, func() {
		NotBeNilf(0, "not nil")
	})
}

func TestNotContainf(t *testing.T) {
	assert.Panics(t, func() {
		NotContainf([]int{1, 2, 3}, 2, "contain")
	})

	assert.NotPanics(t, func() {
		NotContainf([]int{1, 2, 3}, 4, "not contain")
	})
}

func TestFailf(t *testing.T) {
	assert.Panics(t, func() {
		Failf("fail")
	})
}
