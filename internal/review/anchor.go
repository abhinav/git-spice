// Package review defines local review-comment domain values.
package review

import (
	"encoding"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Anchor identifies a file or inclusive postimage line range.
type Anchor struct {
	Path string // required

	// StartLine and EndLine identify an inclusive postimage line range.
	// Both are zero for a file-level anchor.
	StartLine int
	EndLine   int
}

var _ encoding.TextUnmarshaler = (*Anchor)(nil)

// UnmarshalText parses a file, file:line, or file:start-end anchor.
func (a *Anchor) UnmarshalText(src []byte) error {
	value := string(src)
	if value == "" {
		return errors.New("comment anchor is required")
	}

	// Split from the right so paths containing a colon remain intact.
	path, lineSpec, hasLine := strings.CutLast(value, ":")
	if !hasLine {
		// Without a line suffix, the anchor applies to the whole file.
		*a = Anchor{Path: value}
		return nil
	}
	if path == "" {
		return errors.New("comment anchor path is required")
	}

	before, after, hasRange := strings.Cut(lineSpec, "-")
	if !hasRange {
		line, err := strconv.Atoi(lineSpec)
		if err != nil {
			return fmt.Errorf("invalid line number in %q: %w", value, err)
		}
		if line <= 0 {
			return fmt.Errorf(
				"invalid comment anchor %q: comment anchor lines must be positive",
				value,
			)
		}
		// A single line uses the same inclusive representation as a range.
		*a = Anchor{Path: path, StartLine: line, EndLine: line}
		return nil
	}

	start, err := strconv.Atoi(before)
	if err != nil {
		return fmt.Errorf("invalid range start in %q: %w", value, err)
	}
	end, err := strconv.Atoi(after)
	if err != nil {
		return fmt.Errorf("invalid range end in %q: %w", value, err)
	}
	if start <= 0 || end <= 0 {
		return fmt.Errorf(
			"invalid comment anchor %q: comment anchor lines must be positive",
			value,
		)
	}
	if end < start {
		return fmt.Errorf(
			"invalid comment anchor %q: comment anchor end must not precede start",
			value,
		)
	}
	*a = Anchor{Path: path, StartLine: start, EndLine: end}
	return nil
}

// IsFile reports whether the anchor identifies the whole file.
func (a Anchor) IsFile() bool {
	return a.StartLine == 0
}

// IsLine reports whether the anchor identifies exactly one line.
func (a Anchor) IsLine() bool {
	return a.StartLine > 0 && a.StartLine == a.EndLine
}

// String returns the command-line representation of the anchor.
func (a Anchor) String() string {
	if a.IsFile() {
		return a.Path
	}
	if a.IsLine() {
		return fmt.Sprintf("%s:%d", a.Path, a.StartLine)
	}
	return fmt.Sprintf("%s:%d-%d", a.Path, a.StartLine, a.EndLine)
}
