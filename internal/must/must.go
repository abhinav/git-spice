// Package must provides runtime assertions.
// Violation of these assertions indicates a program fault,
// and should cause a crash to prevent operating with invalid data.
package must

import (
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

// BeEmptyMapf panics if m is not an empty map.
func BeEmptyMapf[Map ~map[K]V, K comparable, V any](m Map, format string, args ...any) {
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
func NotBeBlankf[Str ~string](s Str, format string, args ...any) {
	if len(strings.TrimSpace(string(s))) == 0 {
		panicErrorf(format, args...)
	}
}

// NotBeEmptyf panics if es is an empty slice.
func NotBeEmptyf[S ~[]T, T any](es S, format string, args ...any) {
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

// NotContainf panics if e is in es.
func NotContainf[S ~[]T, T comparable](es S, e T, format string, args ...any) {
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
