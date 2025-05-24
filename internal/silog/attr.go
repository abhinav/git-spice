package silog

import "log/slog"

// NonZero returns a slog attribute for a non-zero value.
// If the value is zero, the attribute is not included in the log.
func NonZero[T comparable](name string, value T) slog.Attr {
	var zero T
	if value == zero {
		return slog.Attr{}
	}
	return slog.Any(name, value)
}
