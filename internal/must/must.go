// Package must provides runtime assertions.
// Violation of these assertions indicates a program fault,
// and should cause a crash to prevent operating with invalid data.
package must

import (
	"fmt"
	"strings"
)

func BeEqualf[T comparable](want, got T, format string, args ...any) {
	if want != got {
		panicErrorf("%v\nwant = %v\n got = %v",
			fmt.Errorf(format, args...),
			want,
			got,
		)
	}
}

func NotBeBlankf(s string, format string, args ...any) {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		panicErrorf(format, args...)
	}
}

func NotBeEmptyf[T any](es []T, format string, args ...any) {
	if len(es) == 0 {
		panicErrorf(format, args...)
	}
}

func panicErrorf(format string, args ...any) {
	panic(fmt.Errorf(format, args...))
}
