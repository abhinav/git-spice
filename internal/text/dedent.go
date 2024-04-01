// Package text provides text manipulation functions.
package text

import "strings"

// Dedent removes a common indent from all lines in a string.
// It allows writing multi-line strings in a more readable way.
// For example:
//
//	const s = text.Dedent(`
//		foo
//		  bar
//		baz
//	`)
//
// The result is:
//
//	foo
//	  bar
//	baz
//
// The common indent is the number of leading spaces in the first non-empty line.
// If a line does not have the common prefix, it is reproduced as is,
// except for the last line, which is ignored if it is blank.
func Dedent(s string) string {
	var indent string
	for {
		line, rest, ok := strings.Cut(s, "\n")
		if !ok {
			// No newline. Just trim the leading spaces.
			return strings.TrimLeft(s, " \t")
		}

		// Text to the first non-space character is the indent.
		idx := strings.IndexFunc(line, func(r rune) bool {
			return r != ' ' && r != '\t'
		})

		// If the line is blank (whitespace only), skip it.
		if idx == -1 {
			s = rest
			continue
		}

		indent = line[:idx]
		break
	}

	var out strings.Builder
	for len(s) > 0 {
		line, rest, ok := strings.Cut(s, "\n")
		isLast := !ok
		s = rest

		// Trim the prefix, print, and continue.
		if line, ok := strings.CutPrefix(line, indent); ok {
			if out.Len() > 0 {
				out.WriteByte('\n')
			}
			out.WriteString(line)
			continue
		}

		// If the prefix is missing, the cases are:
		//
		// 1. This is not the last line.
		// 2. This is the last line and it's not blank.
		// 3. This is the last line and it's blank.
		//
		// For all but (3), we print the line as is.
		if !isLast || strings.TrimSpace(line) != "" {
			if out.Len() > 0 {
				out.WriteByte('\n')
			}
			out.WriteString(line)
		}

	}

	return out.String()
}
