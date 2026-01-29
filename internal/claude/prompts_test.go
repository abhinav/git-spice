package claude

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildPrompt(t *testing.T) {
	t.Run("SimpleReplacement", func(t *testing.T) {
		template := "Review changes in {branch} against {base}"
		vars := map[string]string{
			"branch": "feature",
			"base":   "main",
		}

		result := BuildPrompt(template, vars)
		assert.Equal(t, "Review changes in feature against main", result)
	})

	t.Run("MultipleReplacements", func(t *testing.T) {
		template := "{branch} {branch} {base}"
		vars := map[string]string{
			"branch": "foo",
			"base":   "bar",
		}

		result := BuildPrompt(template, vars)
		assert.Equal(t, "foo foo bar", result)
	})

	t.Run("MissingVariable", func(t *testing.T) {
		template := "Review {branch} against {base}"
		vars := map[string]string{
			"branch": "feature",
		}

		result := BuildPrompt(template, vars)
		// Missing variables should remain as-is.
		assert.Equal(t, "Review feature against {base}", result)
	})

	t.Run("NoVariables", func(t *testing.T) {
		template := "Simple prompt with no variables"
		vars := map[string]string{}

		result := BuildPrompt(template, vars)
		assert.Equal(t, "Simple prompt with no variables", result)
	})

	t.Run("ComplexTemplate", func(t *testing.T) {
		template := `Generate PR title and description.
Branch: {branch}, Base: {base}
Commits: {commits}
Diff: {diff}`
		vars := map[string]string{
			"branch":  "feature-x",
			"base":    "main",
			"commits": "abc123 First commit\ndef456 Second commit",
			"diff":    "+added line\n-removed line",
		}

		result := BuildPrompt(template, vars)
		assert.Contains(t, result, "feature-x")
		assert.Contains(t, result, "main")
		assert.Contains(t, result, "abc123")
		assert.Contains(t, result, "+added line")
	})
}

func TestBuildReviewPrompt(t *testing.T) {
	cfg := DefaultConfig()

	t.Run("Basic", func(t *testing.T) {
		prompt := BuildReviewPrompt(cfg, "Fix login bug", "diff content")
		assert.Contains(t, prompt, "Fix login bug")
		assert.Contains(t, prompt, "diff content")
	})
}

func TestBuildSummaryPrompt(t *testing.T) {
	cfg := DefaultConfig()

	t.Run("Basic", func(t *testing.T) {
		prompt := BuildSummaryPrompt(cfg, "feature", "main", "commit messages", "diff")
		assert.Contains(t, prompt, "feature")
		assert.Contains(t, prompt, "main")
		assert.Contains(t, prompt, "commit messages")
		assert.Contains(t, prompt, "diff")
	})
}

func TestBuildCommitPrompt(t *testing.T) {
	cfg := DefaultConfig()

	t.Run("Basic", func(t *testing.T) {
		prompt := BuildCommitPrompt(cfg, "staged diff content")
		assert.Contains(t, prompt, "staged diff content")
	})
}

func TestBuildStackReviewPrompt(t *testing.T) {
	cfg := DefaultConfig()

	t.Run("Basic", func(t *testing.T) {
		branches := "branch1: changes\nbranch2: more changes"
		prompt := BuildStackReviewPrompt(cfg, branches)
		assert.Contains(t, prompt, "branch1")
		assert.Contains(t, prompt, "branch2")
	})
}

func TestRefinePrompt(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		original := "Generate a commit message"
		instruction := "Make it shorter"

		result := RefinePrompt(original, instruction)
		assert.Contains(t, result, original)
		assert.Contains(t, result, instruction)
	})
}

func TestParseTitleBody(t *testing.T) {
	tests := []struct {
		name      string
		response  string
		wantTitle string
		wantBody  string
	}{
		{
			name:      "TitlePrefix",
			response:  "TITLE: Fix login bug\n\nThis fixes the login issue.",
			wantTitle: "Fix login bug",
			wantBody:  "This fixes the login issue.",
		},
		{
			name:      "SubjectPrefix",
			response:  "SUBJECT: Add feature\n\nBODY: Details here.",
			wantTitle: "Add feature",
			wantBody:  "Details here.",
		},
		{
			name:      "CaseInsensitive",
			response:  "title: lowercase title\nbody: lowercase body",
			wantTitle: "lowercase title",
			wantBody:  "lowercase body",
		},
		{
			name:      "Fallback",
			response:  "First line as title\nSecond line as body",
			wantTitle: "First line as title",
			wantBody:  "Second line as body",
		},
		{
			name:      "TitleOnly",
			response:  "TITLE: Just a title",
			wantTitle: "Just a title",
			wantBody:  "",
		},
		{
			name:      "EmptyResponse",
			response:  "",
			wantTitle: "",
			wantBody:  "",
		},
		{
			name:      "MultilineBody",
			response:  "TITLE: PR title\n\nBODY:\nLine 1\nLine 2\nLine 3",
			wantTitle: "PR title",
			wantBody:  "Line 1\nLine 2\nLine 3",
		},
		{
			name: "WithPreamble",
			response: `Based on the diff analysis, here's the PR title and description:

TITLE: Add Claude AI integration

BODY:
This PR adds Claude support.`,
			wantTitle: "Add Claude AI integration",
			wantBody:  "This PR adds Claude support.",
		},
		{
			name: "PreambleWithoutTitlePrefix",
			response: `Based on the changes, here's the summary:

Add new feature for users
This implements the requested functionality.`,
			wantTitle: "Add new feature for users",
			wantBody:  "This implements the requested functionality.",
		},
		{
			name: "HeresPreamble",
			response: `Here's the commit message:

Fix authentication bug
Users can now log in correctly.`,
			wantTitle: "Fix authentication bug",
			wantBody:  "Users can now log in correctly.",
		},
		{
			name: "ColonEndingPreamble",
			response: `I've analyzed the changes and here's what I suggest:

TITLE: Refactor user module
Improves code organization.`,
			wantTitle: "Refactor user module",
			wantBody:  "Improves code organization.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title, body := ParseTitleBody(tt.response)
			assert.Equal(t, tt.wantTitle, title)
			assert.Equal(t, tt.wantBody, body)
		})
	}
}

func TestWrapText(t *testing.T) {
	t.Run("ShortText", func(t *testing.T) {
		result := WrapText("Short text", 72)
		assert.Equal(t, "Short text", result)
	})

	t.Run("LongParagraph", func(t *testing.T) {
		input := "This is a very long paragraph that should be wrapped at the specified width to ensure proper formatting."
		result := WrapText(input, 40)
		for line := range strings.SplitSeq(result, "\n") {
			assert.LessOrEqual(t, len(line), 40)
		}
	})

	t.Run("PreservesLineBreaks", func(t *testing.T) {
		input := "First paragraph.\n\nSecond paragraph."
		result := WrapText(input, 72)
		assert.Contains(t, result, "First paragraph.")
		assert.Contains(t, result, "Second paragraph.")
	})

	t.Run("DefaultWidth", func(t *testing.T) {
		result := WrapText("Test", 0)
		assert.Equal(t, "Test", result)
	})
}

func TestFormatCommitMessage(t *testing.T) {
	t.Run("SubjectOnly", func(t *testing.T) {
		result := FormatCommitMessage("Add new feature", "")
		assert.Equal(t, "Add new feature", result)
	})

	t.Run("SubjectAndBody", func(t *testing.T) {
		result := FormatCommitMessage("Add new feature", "This adds the feature.")
		assert.Equal(t, "Add new feature\n\nThis adds the feature.", result)
	})

	t.Run("LongSubjectTruncated", func(t *testing.T) {
		longSubject := strings.Repeat("word ", 20) // 100 chars
		result := FormatCommitMessage(longSubject, "")
		assert.LessOrEqual(t, len(result), 72)
	})

	t.Run("BodyWrapped", func(t *testing.T) {
		longBody := "This is a long body that should be wrapped properly to ensure each line stays within the 72 character limit for commit messages."
		result := FormatCommitMessage("Subject", longBody)

		// Skip subject and blank line, check body lines.
		lineNum := 0
		for line := range strings.SplitSeq(result, "\n") {
			if lineNum >= 2 { // Skip subject and blank line.
				assert.LessOrEqual(t, len(line), 72)
			}
			lineNum++
		}
	})
}
