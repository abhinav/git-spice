// Package reviewdiff answers review-comment questions about a Git patch.
package reviewdiff

import (
	"bytes"
	"fmt"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

// Patch describes the files and line ranges represented by a Git patch.
//
// Postimage queries use destination paths and line numbers. Deletion queries
// use source paths and line numbers because those lines no longer exist in the
// postimage.
type Patch struct {
	files     map[string][]lineRange
	deletions map[string][]lineRange
}

// Parse parses a Git patch for review-comment queries.
func Parse(src []byte) (*Patch, error) {
	files, _, err := gitdiff.Parse(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("parse Git patch: %w", err)
	}

	patch := &Patch{
		files:     make(map[string][]lineRange),
		deletions: make(map[string][]lineRange),
	}
	for _, file := range files {
		// A destination path is enough to make a file commentable, including
		// binary, rename-only, and mode-only changes without text fragments.
		if file.NewName != "" {
			if _, ok := patch.files[file.NewName]; !ok {
				patch.files[file.NewName] = nil
			}
		}

		for _, fragment := range file.TextFragments {
			if file.NewName != "" && fragment.NewLines > 0 {
				patch.files[file.NewName] = append(
					patch.files[file.NewName],
					lineRange{
						start: fragment.NewPosition,
						end:   fragment.NewPosition + fragment.NewLines - 1,
					},
				)
			}

			// Fragment ranges include context. Walk the old-side cursor so
			// deletion queries distinguish removed lines from nearby context.
			oldLine := fragment.OldPosition
			for _, line := range fragment.Lines {
				switch line.Op {
				case gitdiff.OpContext:
					oldLine++
				case gitdiff.OpDelete:
					if file.OldName != "" {
						patch.deletions[file.OldName] = appendLine(
							patch.deletions[file.OldName],
							oldLine,
						)
					}
					oldLine++
				case gitdiff.OpAdd:
				}
			}
		}
	}

	return patch, nil
}

// ContainsFile reports whether path exists in the patch postimage.
func (p *Patch) ContainsFile(path string) bool {
	_, ok := p.files[path]
	return ok
}

// ContainsLine reports whether the postimage line is in a patch fragment.
func (p *Patch) ContainsLine(path string, line int) bool {
	return p.ContainsLineRange(path, line, line)
}

// ContainsLineRange reports whether every line in the inclusive postimage
// range is in one patch fragment.
func (p *Patch) ContainsLineRange(path string, start, end int) bool {
	if start <= 0 || end < start {
		return false
	}

	for _, fragment := range p.files[path] {
		if fragment.contains(int64(start), int64(end)) {
			return true
		}
	}
	return false
}

// DeletesLine reports whether the patch deletes the source line.
func (p *Patch) DeletesLine(path string, line int) bool {
	return p.DeletesLineRange(path, line, line)
}

// DeletesLineRange reports whether the patch deletes any source line in the
// inclusive range.
func (p *Patch) DeletesLineRange(path string, start, end int) bool {
	if start <= 0 || end < start {
		return false
	}

	for _, deletion := range p.deletions[path] {
		if deletion.overlaps(int64(start), int64(end)) {
			return true
		}
	}
	return false
}

// lineRange is an inclusive interval in one side of a file diff.
type lineRange struct {
	start int64
	end   int64
}

func (r lineRange) contains(start, end int64) bool {
	return r.start <= start && end <= r.end
}

func (r lineRange) overlaps(start, end int64) bool {
	return r.start <= end && start <= r.end
}

// appendLine adds a deleted line while coalescing consecutive lines into a
// compact range.
func appendLine(ranges []lineRange, line int64) []lineRange {
	if len(ranges) == 0 || ranges[len(ranges)-1].end+1 != line {
		return append(ranges, lineRange{start: line, end: line})
	}

	ranges[len(ranges)-1].end = line
	return ranges
}
