package termtest

import (
	"strings"

	"github.com/vito/midterm"
)

// Rows takes a snapshot of a midterm terminal screen.
func Rows(term *midterm.Screen) []string {
	var lines []string
	for _, row := range term.Content {
		rowstr := strings.TrimRight(string(row), " \t\n")
		lines = append(lines, rowstr)
	}

	// Trim trailing empty lines.
	for i := len(lines) - 1; i >= 0; i-- {
		if len(lines[i]) > 0 {
			lines = lines[:i+1]
			break
		}
	}

	return lines
}

// Screen returns a string representation of a terminal screen.
func Screen(term *midterm.Screen) string {
	var s strings.Builder
	for _, row := range term.Content {
		row = trimRightWS(row)
		s.WriteString(string(row))
		s.WriteRune('\n')
	}
	return strings.TrimRight(s.String(), "\n")
}

func trimRightWS(rs []rune) []rune {
	for i := len(rs) - 1; i >= 0; i-- {
		switch rs[i] {
		case ' ', '\t', '\n':
			// next
		default:
			return rs[:i+1]
		}
	}
	return nil
}
