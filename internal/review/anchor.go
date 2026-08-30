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
	path      string
	startLine int
	endLine   int
}

var _ encoding.TextUnmarshaler = (*Anchor)(nil)

// NewFileAnchor returns an anchor for the whole file at path.
func NewFileAnchor(path string) (Anchor, error) {
	if path == "" {
		return Anchor{}, errors.New("comment anchor path is required")
	}
	return Anchor{path: path}, nil
}

// NewLineAnchor returns an anchor for one postimage line.
func NewLineAnchor(path string, line int) (Anchor, error) {
	return NewLineRangeAnchor(path, line, line)
}

// NewLineRangeAnchor returns an anchor for an inclusive postimage line range.
func NewLineRangeAnchor(path string, start, end int) (Anchor, error) {
	if path == "" {
		return Anchor{}, errors.New("comment anchor path is required")
	}
	if start <= 0 || end <= 0 {
		return Anchor{}, errors.New("comment anchor lines must be positive")
	}
	if end < start {
		return Anchor{}, errors.New("comment anchor end must not precede start")
	}
	return Anchor{
		path:      path,
		startLine: start,
		endLine:   end,
	}, nil
}

// UnmarshalText parses a file, file:line, or file:start-end anchor.
func (a *Anchor) UnmarshalText(src []byte) error {
	value := string(src)
	if value == "" {
		return errors.New("comment anchor is required")
	}

	idx := strings.LastIndex(value, ":")
	if idx < 0 {
		parsed, err := NewFileAnchor(value)
		if err != nil {
			return err
		}
		*a = parsed
		return nil
	}

	path := value[:idx]
	lineSpec := value[idx+1:]
	before, after, hasRange := strings.Cut(lineSpec, "-")
	if !hasRange {
		line, err := strconv.Atoi(lineSpec)
		if err != nil {
			return fmt.Errorf("invalid line number in %q: %w", value, err)
		}
		parsed, err := NewLineAnchor(path, line)
		if err != nil {
			return fmt.Errorf("invalid comment anchor %q: %w", value, err)
		}
		*a = parsed
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
	parsed, err := NewLineRangeAnchor(path, start, end)
	if err != nil {
		return fmt.Errorf("invalid comment anchor %q: %w", value, err)
	}
	*a = parsed
	return nil
}

// Path reports the repository-relative file path.
func (a Anchor) Path() string {
	return a.path
}

// StartLine reports the first line in the inclusive range.
// It is zero for a file anchor.
func (a Anchor) StartLine() int {
	return a.startLine
}

// EndLine reports the last line in the inclusive range.
// It is zero for a file anchor.
func (a Anchor) EndLine() int {
	return a.endLine
}

// IsFile reports whether the anchor identifies the whole file.
func (a Anchor) IsFile() bool {
	return a.startLine == 0
}

// IsLine reports whether the anchor identifies exactly one line.
func (a Anchor) IsLine() bool {
	return a.startLine > 0 && a.startLine == a.endLine
}

// String returns the command-line representation of the anchor.
func (a Anchor) String() string {
	if a.IsFile() {
		return a.path
	}
	if a.IsLine() {
		return fmt.Sprintf("%s:%d", a.path, a.startLine)
	}
	return fmt.Sprintf("%s:%d-%d", a.path, a.startLine, a.endLine)
}
