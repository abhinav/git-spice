// Package claude provides integration with Claude AI for code review
// and commit message generation.
package claude

import (
	"path/filepath"
	"regexp"
	"strings"
)

// DiffFile represents a single file's diff content.
type DiffFile struct {
	// Path is the file path relative to the repository root.
	Path string

	// Content is the raw diff content for this file.
	Content string

	// Binary indicates whether this is a binary file.
	Binary bool
}

// BudgetResult contains the result of a budget check.
type BudgetResult struct {
	// OverBudget is true if the diff exceeds the line budget.
	OverBudget bool

	// TotalLines is the total number of lines in the diff.
	TotalLines int

	// MaxLines is the configured maximum line budget.
	MaxLines int

	// FileLines maps file paths to their line counts.
	FileLines map[string]int
}

var (
	// diffHeaderRegex matches the start of a new file diff.
	//
	// Git uses two formats for diff headers:
	//   - Unquoted: diff --git a/file.go b/file.go
	//   - Quoted:   diff --git "a/path with spaces.go" "b/path with spaces.go"
	//
	// The regex uses alternation to match both formats:
	//   Pattern: (?:"?a/(.+?)"? "?b/(.+?)"?|a/(.+?) b/(.+?))
	//            |<---- quoted format ---->|<-- unquoted -->|
	//
	// Capture groups (1-indexed):
	//   [1] quoted source path      - e.g., "path with spaces.go" from "a/..."
	//   [2] quoted destination path - e.g., "path with spaces.go" from "b/..."
	//   [3] unquoted source path    - e.g., "file.go" from a/file.go
	//   [4] unquoted destination    - e.g., "file.go" from b/file.go
	//
	// Either [1,2] or [3,4] will be populated, never both.
	// We use [2] or [4] to get the destination path (the file after changes).
	diffHeaderRegex = regexp.MustCompile(`^diff --git (?:"?a/(.+?)"? "?b/(.+?)"?|a/(.+?) b/(.+?))$`)

	// binaryFileRegex matches binary file markers in git diff output.
	// Example: "Binary files a/image.png and b/image.png differ"
	binaryFileRegex = regexp.MustCompile(`^Binary files .+ and .+ differ$`)

	// filePathRegex matches +++ lines to extract the definitive file path.
	//
	// The +++ line is more reliable than the diff header because it shows
	// the actual destination path. Format:
	//   - Normal:  +++ b/file.go
	//   - Quoted:  +++ "b/path with spaces.go"
	//   - Deleted: +++ /dev/null
	//
	// Capture group [1] contains the path without "b/" prefix.
	// For deleted files (/dev/null), the group is empty.
	filePathRegex = regexp.MustCompile(`^\+\+\+ (?:"?b/(.+?)"?|/dev/null)$`)
)

// Capture group indices for diffHeaderRegex.
// These must be updated if the regex pattern changes.
const (
	diffHeaderQuotedDest   = 2 // "b/path with spaces"
	diffHeaderUnquotedDest = 4 // b/path
)

// ParseDiff parses a unified diff into per-file sections.
func ParseDiff(diff string) ([]DiffFile, error) {
	var files []DiffFile
	lines := strings.Split(diff, "\n")

	var currentFile *DiffFile
	var contentBuilder strings.Builder
	// Pre-allocate capacity: estimate average file size as 1/10 of total.
	contentBuilder.Grow(len(diff) / 10)

	for _, line := range lines {
		// Check for diff header (start of new file).
		if matches := diffHeaderRegex.FindStringSubmatch(line); matches != nil {
			// Save previous file if exists.
			if currentFile != nil {
				currentFile.Content = contentBuilder.String()
				files = append(files, *currentFile)
			}

			// Extract destination path from diff header.
			var destPath string
			if len(matches) > diffHeaderQuotedDest && matches[diffHeaderQuotedDest] != "" {
				destPath = matches[diffHeaderQuotedDest]
			} else if len(matches) > diffHeaderUnquotedDest && matches[diffHeaderUnquotedDest] != "" {
				destPath = matches[diffHeaderUnquotedDest]
			}

			currentFile = &DiffFile{
				Path: destPath,
			}
			contentBuilder.Reset()
			contentBuilder.WriteString(line)
			contentBuilder.WriteByte('\n')
			continue
		}

		// Check for binary file marker.
		if currentFile != nil && binaryFileRegex.MatchString(line) {
			currentFile.Binary = true
			contentBuilder.WriteString(line)
			contentBuilder.WriteByte('\n')
			continue
		}

		// Check for +++ line to get definitive file path.
		if currentFile != nil {
			if matches := filePathRegex.FindStringSubmatch(line); matches != nil {
				if matches[1] != "" {
					currentFile.Path = matches[1]
				}
			}
		}

		// Add line to current file's content.
		if currentFile != nil {
			contentBuilder.WriteString(line)
			contentBuilder.WriteByte('\n')
		}
	}

	// Save last file, trimming trailing newline for consistency.
	if currentFile != nil {
		currentFile.Content = strings.TrimSuffix(contentBuilder.String(), "\n")
		files = append(files, *currentFile)
	}

	return files, nil
}

// FilterDiff filters diff files based on ignore patterns and binary status.
// Binary files are always excluded.
func FilterDiff(files []DiffFile, ignorePatterns []string) []DiffFile {
	var result []DiffFile

	for _, f := range files {
		// Exclude binary files.
		if f.Binary {
			continue
		}

		// Check against ignore patterns.
		if matchesAnyPattern(f.Path, ignorePatterns) {
			continue
		}

		result = append(result, f)
	}

	return result
}

// matchesAnyPattern checks if a path matches any of the given glob patterns.
func matchesAnyPattern(path string, patterns []string) bool {
	// Sanitize path to prevent traversal attacks.
	// Clean the path and reject absolute paths or paths with "..".
	path = filepath.Clean(path)
	if filepath.IsAbs(path) || strings.HasPrefix(path, "..") {
		// Invalid/suspicious path - filter it out for security.
		return true
	}

	for _, pattern := range patterns {
		// Try matching the full path.
		matched, err := filepath.Match(pattern, path)
		if err == nil && matched {
			return true
		}

		// Try matching just the filename.
		matched, err = filepath.Match(pattern, filepath.Base(path))
		if err == nil && matched {
			return true
		}

		// Handle directory patterns like "vendor/*".
		if prefix, ok := strings.CutSuffix(pattern, "/*"); ok {
			if strings.HasPrefix(path, prefix+"/") {
				return true
			}
		}
	}
	return false
}

// CheckBudget checks if the diff is within the line budget.
func CheckBudget(files []DiffFile, maxLines int) BudgetResult {
	result := BudgetResult{
		MaxLines:  maxLines,
		FileLines: make(map[string]int),
	}

	for _, f := range files {
		lineCount := countLines(f.Content)
		result.FileLines[f.Path] = lineCount
		result.TotalLines += lineCount
	}

	result.OverBudget = result.TotalLines > maxLines
	return result
}

// countLines counts the number of lines in a string.
func countLines(s string) int {
	if s == "" {
		return 0
	}

	count := strings.Count(s, "\n")

	// If the string doesn't end with a newline, add 1 for the last line.
	if !strings.HasSuffix(s, "\n") {
		count++
	}

	return count
}

// ReconstructDiff reconstructs a diff from filtered file sections.
func ReconstructDiff(files []DiffFile) string {
	if len(files) == 0 {
		return ""
	}

	// Pre-allocate exact capacity: sum of content lengths + (n-1) separators.
	totalLen := 0
	for _, f := range files {
		totalLen += len(f.Content)
	}
	totalLen += len(files) - 1 // newline separators between files

	var builder strings.Builder
	builder.Grow(totalLen)

	for i, f := range files {
		if i > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(f.Content)
	}

	return builder.String()
}

// FilteredDiffResult holds the result of parsing and filtering a diff.
type FilteredDiffResult struct {
	// Files is the list of filtered diff files.
	Files []DiffFile
	// Budget is the budget check result.
	Budget BudgetResult
	// FilteredDiff is the reconstructed diff text.
	FilteredDiff string
}

// ParseAndFilterDiff parses a diff, filters it, and checks the budget.
// Returns the filtered files, budget info, and reconstructed diff.
// Does not return an error for over-budget - callers should check Budget.OverBudget.
func ParseAndFilterDiff(diffText string, cfg *Config) (*FilteredDiffResult, error) {
	files, err := ParseDiff(diffText)
	if err != nil {
		return nil, err
	}

	filtered := FilterDiff(files, cfg.IgnorePatterns)
	budget := CheckBudget(filtered, cfg.MaxLines)

	return &FilteredDiffResult{
		Files:        filtered,
		Budget:       budget,
		FilteredDiff: ReconstructDiff(filtered),
	}, nil
}
