// Package must provides runtime assertions.
// Violation of these assertions indicates a program fault,
// and should cause a crash to prevent operating with invalid data.
package must

import (
	"cmp"
	"fmt"
	"strings"
)

// Bef panics if b is false.
func Bef(b bool, format string, args ...any) {
	if !b {
		panicErrorf(format, args...)
	}
}

// NotBef panics if b is true.
func NotBef(b bool, format string, args ...any) {
	if b {
		panicErrorf(format, args...)
	}
}

// BeEqualf panics if a != b.
func BeEqualf[T comparable](a, b T, format string, args ...any) {
	if a != b {
		panicErrorf("%v\nwant a == b\na = %v\nb = %v",
			fmt.Errorf(format, args...), a, b,
		)
	}
}

// BeInRangef panics if v is not in the range [min, max).
func BeInRangef[T cmp.Ordered](v, min, max T, format string, args ...any) {
	if v < min || v >= max {
		panicErrorf("%v\nwant %v <= v < %v\nv = %v",
			fmt.Errorf(format, args...), min, max, v)
	}
}

// BeEmptyMapf panics if m is not an empty map.
func BeEmptyMapf[K comparable, V any](m map[K]V, format string, args ...any) {
	if len(m) != 0 {
		panicErrorf("%v\ngot: %v", fmt.Errorf(format, args...), m)
	}
}

// NotBeEqualf panics if a == b.
func NotBeEqualf[T comparable](a, b T, format string, args ...any) {
	if a == b {
		panicErrorf("%v\nwant a != b\na = %v\nb = %v",
			fmt.Errorf(format, args...), a, b)
	}
}

// NotBeBlankf panics if s is empty or contains only whitespace.
func NotBeBlankf(s string, format string, args ...any) {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		panicErrorf(format, args...)
	}
}

// NotBeEmptyf panics if es is an empty slice.
func NotBeEmptyf[T any](es []T, format string, args ...any) {
	if len(es) == 0 {
		panicErrorf(format, args...)
	}
}

// NotBeZerof panics if v is the zero value.
func NotBeZerof[T comparable](v T, format string, args ...any) {
	var zero T
	if v == zero {
		panicErrorf(format, args...)
	}
}

// NotBeNilf panics if v is nil.
func NotBeNilf(v any, format string, args ...any) {
	if v == nil {
		panicErrorf(format, args...)
	}
}

// NotContainf panics if e is in es.
func NotContainf[T comparable](es []T, e T, format string, args ...any) {
	for _, x := range es {
		if x == e {
			panicErrorf(format, args...)
		}
	}
}

// Failf unconditionally panics with the given message.
func Failf(format string, args ...any) {
	panicErrorf(format, args...)
}

func panicErrorf(format string, args ...any) {
	panic(fmt.Errorf(format, args...))
}
