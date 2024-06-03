// Package cmputil provides utilities for comparing values.
package cmputil

// Zero reports whether v is the zero value for its type.
func Zero[T comparable](v T) bool {
	var zero T
	return v == zero
}
