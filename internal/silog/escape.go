package silog

import (
	"strconv"
	"strings"
	"unicode"
)

// MaybeQuote quotes the given string and escapes special characters
// if it would benefit from quoting.
// Otherwise, it returns the string unchanged.
func MaybeQuote(s string) string {
	if needsQuoting(s) {
		return strconv.Quote(s)
	}
	return s
}

// needsQuoting returns true if the string contains invisible characters
// or characters that don't fit on the same line.
func needsQuoting(s string) bool {
	if s == "" {
		return false
	}

	// Quote strings that are only whitespace (like " " or "  ")
	if strings.TrimSpace(s) == "" {
		return true
	}

	// Quote strings with control characters or non-printable characters
	for _, r := range s {
		if unicode.IsControl(r) || !unicode.IsPrint(r) {
			return true
		}
	}

	return false
}
