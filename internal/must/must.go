// Package must provides runtime assertions.
// Violation of these assertions indicates a program fault,
// and should cause a crash to prevent operating with invalid data.
package must

import (
	"cmp"
	"fmt"
	"strings"
)

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

// NotBeNilf panics if v is nil.
func NotBeNilf(v any, format string, args ...any) {
	if v == nil {
		panicErrorf(format, args...)
	}
}

// Failf unconditionally panics with the given message.
func Failf(format string, args ...any) {
	panicErrorf(format, args...)
}

func panicErrorf(format string, args ...any) {
	panic(fmt.Errorf(format, args...))
}
