// Package diffmap maps working-tree line numbers
// to diff hunk positions for inline code review comments.
package diffmap

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Mapper maps file:line references to diff coordinates.
type Mapper struct {
	// files maps file paths to their diff hunks.
	files map[string]*fileDiff
}

type fileDiff struct {
	// hunks are the diff hunks for the file,
	// in order of appearance.
	hunks []hunk
}

type hunk struct {
	// newStart is the starting line number
	// in the new version of the file.
	newStart int

	// newCount is the number of lines
	// in the new version of the hunk.
	newCount int

	// oldStart is the starting line number
	// in the old version of the file.
	oldStart int

	// oldCount is the number of lines
	// in the old version of the hunk.
	oldCount int

	// lines are the diff lines in the hunk.
	lines []diffLine
}

type diffLine struct {
	// op is the diff operation: ' ', '+', or '-'.
	op byte

	// newLineNo is the line number in the new file.
	// Zero for deleted lines.
	newLineNo int

	// oldLineNo is the line number in the old file.
	// Zero for added lines.
	oldLineNo int
}

// New creates a Mapper from unified diff output.
// The diff should be the output of git diff base...HEAD.
func New(diff []byte) (*Mapper, error) {
	m := &Mapper{
		files: make(map[string]*fileDiff),
	}

	scanner := bufio.NewScanner(bytes.NewReader(diff))
	var currentFile string
	var currentHunk *hunk

	for scanner.Scan() {
		line := scanner.Text()

		// Detect file header: "diff --git a/path b/path"
		if strings.HasPrefix(line, "diff --git ") {
			currentFile = ""
			currentHunk = nil
			continue
		}

		// Detect new file path: "+++ b/path"
		if path, ok := strings.CutPrefix(line, "+++ b/"); ok {
			currentFile = path
			if _, ok := m.files[currentFile]; !ok {
				m.files[currentFile] = &fileDiff{}
			}
			continue
		}

		// Detect rename/copy target:
		// "rename to path" or "copy to path"
		if path, ok := strings.CutPrefix(line, "rename to "); ok {
			currentFile = path
			if _, ok := m.files[currentFile]; !ok {
				m.files[currentFile] = &fileDiff{}
			}
			continue
		}
		if path, ok := strings.CutPrefix(line, "copy to "); ok {
			currentFile = path
			if _, ok := m.files[currentFile]; !ok {
				m.files[currentFile] = &fileDiff{}
			}
			continue
		}

		// Skip if no file context yet.
		if currentFile == "" {
			continue
		}

		// Detect hunk header: "@@ -old,count +new,count @@"
		if strings.HasPrefix(line, "@@ ") {
			h, err := parseHunkHeader(line)
			if err != nil {
				return nil, fmt.Errorf(
					"parse hunk header %q: %w",
					line, err,
				)
			}
			fd := m.files[currentFile]
			fd.hunks = append(fd.hunks, h)
			currentHunk = &fd.hunks[len(fd.hunks)-1]
			continue
		}

		// Process diff lines within a hunk.
		if currentHunk == nil || len(line) == 0 {
			continue
		}

		op := line[0]
		switch op {
		case ' ':
			// Context line: present in both old and new.
			dl := diffLine{
				op:        ' ',
				oldLineNo: currentHunk.oldStart + countOp(currentHunk.lines, ' ', '-'),
				newLineNo: currentHunk.newStart + countOp(currentHunk.lines, ' ', '+'),
			}
			currentHunk.lines = append(currentHunk.lines, dl)
		case '+':
			// Added line: only in new.
			dl := diffLine{
				op:        '+',
				newLineNo: currentHunk.newStart + countOp(currentHunk.lines, ' ', '+'),
			}
			currentHunk.lines = append(currentHunk.lines, dl)
		case '-':
			// Deleted line: only in old.
			dl := diffLine{
				op:        '-',
				oldLineNo: currentHunk.oldStart + countOp(currentHunk.lines, ' ', '-'),
			}
			currentHunk.lines = append(currentHunk.lines, dl)
		case '\\':
			// "\ No newline at end of file" — skip.
		default:
			// Unknown line type — skip.
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan diff: %w", err)
	}

	return m, nil
}

// Map converts a working-tree file:line reference
// to diff coordinates suitable for forge inline comments.
//
// Returns the file path, the diff line number,
// and the side ("LEFT" or "RIGHT").
//
// Returns an error if the file or line
// is not part of the diff.
func (m *Mapper) Map(file string, line int) (
	path string, diffLine int, side string, err error,
) {
	fd, ok := m.files[file]
	if !ok {
		return "", 0, "",
			fmt.Errorf("file %q not in diff", file)
	}

	for _, h := range fd.hunks {
		for _, dl := range h.lines {
			if dl.newLineNo == line &&
				(dl.op == '+' || dl.op == ' ') {
				return file, dl.newLineNo, "RIGHT", nil
			}
		}
	}

	return "", 0, "", fmt.Errorf(
		"line %d of %q not in diff", line, file,
	)
}

// Files returns all file paths present in the diff.
func (m *Mapper) Files() []string {
	var files []string
	for f := range m.files {
		files = append(files, f)
	}
	return files
}

// LineModified reports whether the given file:line was changed
// by this diff. side selects which side of the diff to inspect:
// "LEFT" (or "left") tests the NEW range of each hunk (the diff's
// "+" side), "RIGHT" (or "right", and the default) tests the OLD
// range (the diff's "-" side).
//
// The "RIGHT"-tests-OLD convention exists because callers ask
// stale-style questions: "this comment was anchored to line N on
// the RIGHT side of the original PR diff (i.e., the post-commit
// version of the file). I am now looking at the diff between the
// post-commit and the current head — has line N been touched in
// the post-commit's frame of reference?" That frame is the OLD
// side of the post-commit..head diff.
func (m *Mapper) LineModified(file string, line int, side string) bool {
	fd, ok := m.files[file]
	if !ok {
		return false
	}
	useNewRange := strings.EqualFold(side, "LEFT")
	for _, h := range fd.hunks {
		if useNewRange {
			if h.newCount > 0 &&
				line >= h.newStart &&
				line < h.newStart+h.newCount {
				return true
			}
		} else {
			if h.oldCount > 0 &&
				line >= h.oldStart &&
				line < h.oldStart+h.oldCount {
				return true
			}
		}
	}
	return false
}

// parseHunkHeader parses "@@ -old,count +new,count @@".
func parseHunkHeader(line string) (hunk, error) {
	// Strip "@@ " prefix and " @@..." suffix.
	line = strings.TrimPrefix(line, "@@ ")
	idx := strings.Index(line, " @@")
	if idx < 0 {
		return hunk{},
			errors.New("missing @@ terminator")
	}
	line = line[:idx]

	parts := strings.SplitN(line, " ", 2)
	if len(parts) != 2 {
		return hunk{},
			errors.New("expected old and new ranges")
	}

	oldStart, oldCount, err := parseRange(
		strings.TrimPrefix(parts[0], "-"),
	)
	if err != nil {
		return hunk{},
			fmt.Errorf("parse old range: %w", err)
	}

	newStart, newCount, err := parseRange(
		strings.TrimPrefix(parts[1], "+"),
	)
	if err != nil {
		return hunk{},
			fmt.Errorf("parse new range: %w", err)
	}

	return hunk{
		oldStart: oldStart,
		oldCount: oldCount,
		newStart: newStart,
		newCount: newCount,
	}, nil
}

// parseRange parses "start,count" or "start" (count=1).
func parseRange(s string) (start, count int, err error) {
	parts := strings.SplitN(s, ",", 2)
	start, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0,
			fmt.Errorf("parse start: %w", err)
	}
	if len(parts) == 1 {
		return start, 1, nil
	}
	count, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0,
			fmt.Errorf("parse count: %w", err)
	}
	return start, count, nil
}

// countOp counts lines in the hunk
// that match any of the given operations.
func countOp(lines []diffLine, ops ...byte) int {
	n := 0
	for _, l := range lines {
		if slices.Contains(ops, l.op) {
			n++
		}
	}
	return n
}
