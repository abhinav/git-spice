package reviewdiff

import (
	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"go.abhg.dev/gs/internal/review"
)

// MapAnchor maps an anchor in the patch preimage to its corresponding
// postimage region. It reports false when the attached file or region is
// deleted without replacement.
//
// An anchor for a file absent from the patch is unchanged. Insertions within a
// line range expand the range, deletions shrink it, and replacements map to the
// replacement region.
func (p *Patch) MapAnchor(anchor review.Anchor) (review.Anchor, bool) {
	var zero review.Anchor

	mapping, ok := p.mappings[anchor.Path]
	if !ok {
		return anchor, true
	}
	if mapping.newPath == "" {
		return zero, false
	}

	anchor.Path = mapping.newPath
	if anchor.IsFile() {
		return anchor, true
	}

	start, end, ok := mapping.mapRange(anchor.StartLine, anchor.EndLine)
	if !ok {
		return zero, false
	}
	anchor.StartLine = start
	anchor.EndLine = end
	return anchor, true
}

// fileMapping describes how one preimage file becomes its postimage file.
// An empty newPath means that the file was deleted.
type fileMapping struct {
	newPath string
	edits   []lineEdit
}

// newFileMapping translates parsed diff fragments into ordered replacement
// operations against preimage line numbers.
func newFileMapping(file *gitdiff.File) fileMapping {
	mapping := fileMapping{newPath: file.NewName}
	for _, fragment := range file.TextFragments {
		oldLine := int(fragment.OldPosition)
		if fragment.OldLines == 0 {
			// A zero-length hunk identifies the line before an insertion.
			oldLine++
		}

		// Adjacent deletions and additions form one replacement. Context lines
		// end the current replacement and advance both sides of the diff.
		var edit lineEdit
		appendEdit := func() {
			if edit.deleted == 0 && edit.added == 0 {
				return
			}
			mapping.edits = append(mapping.edits, edit)
			edit = lineEdit{}
		}
		for _, line := range fragment.Lines {
			switch line.Op {
			case gitdiff.OpContext:
				appendEdit()
				oldLine++
			case gitdiff.OpDelete:
				if edit.deleted == 0 && edit.added == 0 {
					edit.oldStart = oldLine
				}
				edit.deleted++
				oldLine++
			case gitdiff.OpAdd:
				if edit.deleted == 0 && edit.added == 0 {
					edit.oldStart = oldLine
				}
				edit.added++
			}
		}
		appendEdit()
	}
	return mapping
}

// mapRange applies each replacement in source order. It reports false only
// when deletions consume the complete selected range without replacing it.
func (m fileMapping) mapRange(start, end int) (int, int, bool) {
	var offset int
	for _, edit := range m.edits {
		// Earlier edits move later preimage coordinates by their net line count.
		position := edit.oldStart + offset
		if edit.deleted == 0 {
			// An insertion before the range shifts it. An insertion strictly
			// inside the range becomes part of the selected region.
			switch {
			case position <= start:
				start += edit.added
				end += edit.added
			case position <= end:
				end += edit.added
			}
			offset += edit.added
			continue
		}

		deletedEnd := position + edit.deleted - 1
		switch {
		case deletedEnd < start:
			start += edit.added - edit.deleted
			end += edit.added - edit.deleted
		case position > end:
		default:
			// Preserve surviving selected lines around the edit and include any
			// replacement lines when the edit overlaps the selected region.
			keepsLeft := start < position
			keepsRight := end > deletedEnd
			if edit.added == 0 && !keepsLeft && !keepsRight {
				return 0, 0, false
			}
			if !keepsLeft {
				start = position
			}
			if keepsRight {
				end += edit.added - edit.deleted
			} else {
				end = position + edit.added - 1
			}
		}
		offset += edit.added - edit.deleted
	}
	return start, end, true
}

// lineEdit replaces deleted preimage lines with added postimage lines.
type lineEdit struct {
	oldStart int
	deleted  int
	added    int
}
