package claude

import (
	"strings"
	"unicode"
)

// commitLineWidth is the conventional Git commit message line width.
// This is a widely-adopted standard: git log, GitHub, etc. expect this width.
const commitLineWidth = 72

// BuildPrompt replaces placeholders in a template with provided values.
// Placeholders are in the format {key}.
// Missing keys are left as-is.
func BuildPrompt(template string, vars map[string]string) string {
	// Estimate output size for pre-allocation.
	estimatedSize := len(template)
	for _, value := range vars {
		estimatedSize += len(value)
	}

	var result strings.Builder
	result.Grow(estimatedSize)

	// Process template character by character, looking for placeholders.
	i := 0
	for i < len(template) {
		if template[i] == '{' {
			// Look for closing brace.
			end := strings.IndexByte(template[i+1:], '}')
			if end != -1 {
				key := template[i+1 : i+1+end]
				if value, ok := vars[key]; ok {
					result.WriteString(value)
					i += end + 2 // Skip past {key}
					continue
				}
			}
		}
		result.WriteByte(template[i])
		i++
	}

	return result.String()
}

// BuildReviewPrompt builds a code review prompt.
func BuildReviewPrompt(cfg *Config, title, diff string) string {
	return BuildPrompt(cfg.Prompts.Review, map[string]string{
		"title": title,
		"diff":  diff,
	})
}

// BuildSummaryPrompt builds a PR summary generation prompt.
func BuildSummaryPrompt(cfg *Config, branch, base, commits, diff string) string {
	return BuildPrompt(cfg.Prompts.Summary, map[string]string{
		"branch":  branch,
		"base":    base,
		"commits": commits,
		"diff":    diff,
	})
}

// BuildCommitPrompt builds a commit message generation prompt.
func BuildCommitPrompt(cfg *Config, diff string) string {
	return BuildPrompt(cfg.Prompts.Commit, map[string]string{
		"diff": diff,
	})
}

// BuildStackReviewPrompt builds a stack review prompt.
func BuildStackReviewPrompt(cfg *Config, branches string) string {
	return BuildPrompt(cfg.Prompts.StackReview, map[string]string{
		"branches": branches,
	})
}

// RefinePrompt appends a refinement instruction to an original prompt.
func RefinePrompt(original, instruction string) string {
	return original + "\n\nAdditional instruction: " + instruction
}

// ParseTitleBody extracts title and body from Claude's response.
// It looks for "TITLE:" or "SUBJECT:" prefixes, with optional "BODY:" prefix.
// Falls back to using first non-preamble line as title, rest as body.
func ParseTitleBody(response string) (title, body string) {
	lines := strings.Split(strings.TrimSpace(response), "\n")
	if len(lines) == 0 {
		return response, ""
	}

	// Look for TITLE: or SUBJECT: prefix anywhere in the response.
	for i, line := range lines {
		lineLower := strings.ToLower(strings.TrimSpace(line))

		var prefixLen int
		switch {
		case strings.HasPrefix(lineLower, "title:"):
			prefixLen = 6
		case strings.HasPrefix(lineLower, "subject:"):
			prefixLen = 8
		default:
			continue
		}

		// Found the title line - extract title after prefix.
		trimmedLine := strings.TrimSpace(line)
		title = strings.TrimSpace(trimmedLine[prefixLen:])

		if i+1 < len(lines) {
			// Process remaining lines for body.
			remaining := lines[i+1:]
			remaining = extractBody(remaining)
			body = strings.TrimSpace(strings.Join(remaining, "\n"))
		}
		return title, body
	}

	// Fallback: skip common preamble patterns and use first real content line.
	return parseFallback(lines)
}

// extractBody processes remaining lines after the title line.
// It skips empty lines and handles BODY: prefix.
func extractBody(lines []string) []string {
	for i, line := range lines {
		lineLower := strings.ToLower(strings.TrimSpace(line))

		if strings.HasPrefix(lineLower, "body:") {
			// Extract body content after "body:" prefix.
			bodyContent := strings.TrimSpace(line[5:])
			if bodyContent != "" {
				return append([]string{bodyContent}, lines[i+1:]...)
			}
			return lines[i+1:]
		}

		// Skip empty lines before body content.
		if strings.TrimSpace(line) != "" {
			return lines[i:]
		}
	}
	return nil
}

// parseFallback handles responses without TITLE:/SUBJECT: prefixes.
// It skips common preamble patterns like "Based on..." or "Here's...".
func parseFallback(lines []string) (title, body string) {
	startIdx := 0

	// Skip lines that look like preamble.
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		lower := strings.ToLower(trimmed)
		if isPreambleLine(lower) {
			startIdx = i + 1
			continue
		}

		// Found first non-preamble line.
		startIdx = i
		break
	}

	if startIdx >= len(lines) {
		// All lines were preamble, use first non-empty line.
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				return strings.TrimSpace(line), ""
			}
		}
		return "", ""
	}

	title = strings.TrimSpace(lines[startIdx])
	if startIdx+1 < len(lines) {
		body = strings.TrimSpace(strings.Join(lines[startIdx+1:], "\n"))
	}
	return title, body
}

// isPreambleLine checks if a line looks like Claude's preamble text.
//
// NOTE: This heuristic is fragile and may need updates if Claude changes
// its output format. The prompts request specific formats (TITLE:/BODY:),
// but Claude occasionally adds introductory text. If parsing issues occur,
// check if Claude is using new preamble patterns and add them here.
func isPreambleLine(lower string) bool {
	preamblePrefixes := []string{
		"based on",
		"here's",
		"here is",
		"i've analyzed",
		"i have analyzed",
		"after reviewing",
		"looking at",
		"from the diff",
		"analyzing the",
	}

	for _, prefix := range preamblePrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}

	// Also skip lines that end with colons (likely introductory statements).
	if strings.HasSuffix(lower, ":") && len(lower) > 20 {
		return true
	}

	return false
}

// WrapText wraps text to the specified width, preserving existing line breaks.
// Used for formatting commit message bodies.
func WrapText(text string, width int) string {
	if width <= 0 {
		width = commitLineWidth
	}

	var result strings.Builder
	result.Grow(len(text))

	first := true
	for text != "" {
		var line string
		line, text, _ = strings.Cut(text, "\n")

		if !first {
			result.WriteByte('\n')
		}
		first = false

		// Preserve empty lines.
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Wrap this paragraph.
		result.WriteString(wrapParagraph(line, width))
	}

	return result.String()
}

// wrapParagraph wraps a single paragraph to the specified width.
func wrapParagraph(text string, width int) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}

	var result strings.Builder
	lineLen := 0

	for i, word := range words {
		wordLen := len(word)

		if i == 0 {
			result.WriteString(word)
			lineLen = wordLen
			continue
		}

		// Check if adding this word exceeds width.
		if lineLen+1+wordLen > width {
			result.WriteByte('\n')
			result.WriteString(word)
			lineLen = wordLen
		} else {
			result.WriteByte(' ')
			result.WriteString(word)
			lineLen += 1 + wordLen
		}
	}

	return result.String()
}

// FormatCommitMessage formats a commit subject and body into a proper message.
// The subject is truncated to 72 chars if needed, and the body is wrapped.
// Returns empty string if both subject and body are empty.
func FormatCommitMessage(subject, body string) string {
	subject = strings.TrimSpace(subject)
	body = strings.TrimSpace(body)

	// Handle empty subject by using first line of body.
	if subject == "" {
		if body == "" {
			return ""
		}
		lines := strings.SplitN(body, "\n", 2)
		subject = strings.TrimSpace(lines[0])
		if len(lines) > 1 {
			body = strings.TrimSpace(lines[1])
		} else {
			body = ""
		}
	}

	subject = truncateSubject(subject, commitLineWidth)

	// Validate: subject must be non-empty after processing.
	if subject == "" {
		return ""
	}

	if body == "" {
		return subject
	}
	return subject + "\n\n" + WrapText(body, commitLineWidth)
}

// truncateSubject truncates a commit subject to maxLen runes.
// Prefers breaking at word boundaries when possible.
func truncateSubject(subject string, maxLen int) string {
	runes := []rune(subject)
	if len(runes) <= maxLen {
		return subject
	}

	// Find last space before limit to avoid cutting words.
	cutoff := maxLen
	for i := maxLen - 1; i > 0; i-- {
		if unicode.IsSpace(runes[i]) {
			cutoff = i
			break
		}
	}

	result := strings.TrimSpace(string(runes[:cutoff]))

	// Enforce hard limit even after TrimSpace.
	if runes := []rune(result); len(runes) > maxLen {
		result = string(runes[:maxLen])
	}
	return result
}
