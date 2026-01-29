package claude

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds the Claude AI integration configuration.
// Config holds Claude integration configuration.
//
// Configuration merging: When loading from a file, zero values (0, "", nil)
// are treated as "not set" and default values are preserved. This allows
// partial configuration files. Limitation: you cannot explicitly set a field
// to its zero value (e.g., MaxLines=0 is treated as "use default").
type Config struct {
	// MaxLines is the maximum number of diff lines to send to Claude.
	// Default: 4000. Set to 0 in file to keep default (not to disable).
	MaxLines int `yaml:"maxLines"`

	// IgnorePatterns is a list of glob patterns for files to exclude.
	// Default includes *.lock, *.sum, vendor/*, etc.
	IgnorePatterns []string `yaml:"ignorePatterns"`

	// Models configures which Claude model to use for different operations.
	Models Models `yaml:"models"`

	// Prompts contains the prompt templates for different operations.
	Prompts Prompts `yaml:"prompts"`

	// RefineOptions is a list of quick refinement options.
	RefineOptions []RefineOption `yaml:"refineOptions"`
}

// Models configures which Claude model to use for different operations.
type Models struct {
	// Review is the model for code review (default: claude-sonnet-4-20250514).
	Review string `yaml:"review"`

	// Summary is the model for PR/commit summaries (default: claude-haiku).
	Summary string `yaml:"summary"`

	// Commit is the model for commit messages (default: claude-haiku).
	Commit string `yaml:"commit"`
}

// Prompts contains prompt templates for Claude operations.
type Prompts struct {
	// Review is the prompt template for code review.
	Review string `yaml:"review"`

	// Summary is the prompt template for PR summary generation.
	Summary string `yaml:"summary"`

	// Commit is the prompt template for commit message generation.
	Commit string `yaml:"commit"`

	// StackReview is the prompt template for stack review.
	StackReview string `yaml:"stackReview"`
}

// RefineOption is a quick refinement option for user selection.
type RefineOption struct {
	// Label is the display label for this option.
	Label string `yaml:"label"`

	// Prompt is the instruction to append for refinement.
	Prompt string `yaml:"prompt"`
}

// DefaultTimeout is the default timeout for Claude API calls.
const DefaultTimeout = 5 * time.Minute

// Model constants for Claude CLI.
// Format: claude-{model}-{major}-{minor}-{date}
// These should be updated when new model versions are released.
// Users can override these in their config file.
const (
	ModelSonnet = "claude-sonnet-4-5-20250929"
	ModelHaiku  = "claude-haiku-4-5-20251001"
)

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		MaxLines: 4000,
		IgnorePatterns: []string{
			"*.lock",
			"*.sum",
			"*.min.js",
			"*.min.css",
			"*.svg",
			"*.pb.go",
			"*_generated.go",
			"vendor/*",
			"node_modules/*",
		},
		Models: Models{
			Review:  ModelSonnet, // Sonnet for thorough code review
			Summary: ModelHaiku,  // Haiku for fast summary generation
			Commit:  ModelHaiku,  // Haiku for fast commit messages
		},
		Prompts: Prompts{
			Review:      defaultReviewPrompt,
			Summary:     defaultSummaryPrompt,
			Commit:      defaultCommitPrompt,
			StackReview: defaultStackReviewPrompt,
		},
		RefineOptions: []RefineOption{
			{
				Label:  "Make it shorter",
				Prompt: "Under 100 words.",
			},
			{
				Label:  "Conventional commits",
				Prompt: "Use feat:/fix:/docs:/chore: format.",
			},
			{
				Label:  "More detail",
				Prompt: "Add more technical detail about the implementation.",
			},
			{
				Label:  "Focus on why",
				Prompt: "Focus more on why these changes are needed.",
			},
		},
	}
}

// DefaultConfigPath returns the default configuration file path.
func DefaultConfigPath() string {
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		configDir = filepath.Join(home, ".config")
	}
	return filepath.Join(configDir, "git-spice", "claude.yaml")
}

// LoadConfig loads configuration from the given path.
// If the file does not exist, returns the default configuration.
//
// Configuration merging: File values override defaults only when non-zero.
// Zero values (0, "", nil, empty slice) are treated as "not set" and
// default values are preserved. This allows partial configuration files
// that only specify the values the user wants to change.
func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config file: %w", err)
	}

	// Parse into a separate struct to merge with defaults.
	var fileCfg Config
	if err := yaml.Unmarshal(data, &fileCfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	// Merge file config with defaults.
	// Non-zero file values override defaults; zero values preserve defaults.
	if fileCfg.MaxLines != 0 {
		cfg.MaxLines = fileCfg.MaxLines
	}
	if len(fileCfg.IgnorePatterns) > 0 {
		cfg.IgnorePatterns = fileCfg.IgnorePatterns
	}
	if fileCfg.Models.Review != "" {
		cfg.Models.Review = fileCfg.Models.Review
	}
	if fileCfg.Models.Summary != "" {
		cfg.Models.Summary = fileCfg.Models.Summary
	}
	if fileCfg.Models.Commit != "" {
		cfg.Models.Commit = fileCfg.Models.Commit
	}
	if fileCfg.Prompts.Review != "" {
		cfg.Prompts.Review = fileCfg.Prompts.Review
	}
	if fileCfg.Prompts.Summary != "" {
		cfg.Prompts.Summary = fileCfg.Prompts.Summary
	}
	if fileCfg.Prompts.Commit != "" {
		cfg.Prompts.Commit = fileCfg.Prompts.Commit
	}
	if fileCfg.Prompts.StackReview != "" {
		cfg.Prompts.StackReview = fileCfg.Prompts.StackReview
	}
	if len(fileCfg.RefineOptions) > 0 {
		cfg.RefineOptions = fileCfg.RefineOptions
	}

	return cfg, nil
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.MaxLines <= 0 {
		return errors.New("maxLines must be positive")
	}
	if c.Models.Review == "" {
		return errors.New("models.review must be set")
	}
	if c.Models.Summary == "" {
		return errors.New("models.summary must be set")
	}
	if c.Models.Commit == "" {
		return errors.New("models.commit must be set")
	}
	if c.Prompts.Review == "" {
		return errors.New("prompts.review must be set")
	}
	if c.Prompts.Summary == "" {
		return errors.New("prompts.summary must be set")
	}
	if c.Prompts.Commit == "" {
		return errors.New("prompts.commit must be set")
	}

	// Validate required placeholders in prompts.
	if err := validatePlaceholders(c.Prompts.Review, "prompts.review", "{diff}"); err != nil {
		return err
	}
	if err := validatePlaceholders(c.Prompts.Summary, "prompts.summary", "{diff}"); err != nil {
		return err
	}
	if err := validatePlaceholders(c.Prompts.Commit, "prompts.commit", "{diff}"); err != nil {
		return err
	}

	return nil
}

// validatePlaceholders checks that a prompt contains all required placeholders.
func validatePlaceholders(prompt, name string, required ...string) error {
	for _, p := range required {
		if !strings.Contains(prompt, p) {
			return fmt.Errorf("%s must contain %s placeholder", name, p)
		}
	}
	return nil
}

const defaultReviewPrompt = `Review PR: "{title}"

## Guidelines
1. Code Quality - readability, naming, structure
2. Functionality - correctness, edge cases, bugs
3. Performance - efficiency, memory

## Output
Use suggestion blocks. Be succinct, direct, actionable.

### Changes Requested
- [ ] ...

## Diff:
{diff}`

const defaultSummaryPrompt = `Generate PR title and description for the following changes.
Branch: {branch}, Base: {base}
Commits: {commits}
Diff: {diff}

Use this format:
TITLE: <max 72 chars, imperative mood>
BODY:
# Summary

## Why
Describe *why* this PR is needed.

## What
Describe *what* this PR does.

## Test Plan
- [ ] Build passes with this PR
- [ ] Unit tests pass
- [ ] Ran offline tests (describe commands)
- [ ] Ran online tests (if applicable)`

const defaultCommitPrompt = `Generate a git commit message for the following diff.

Output ONLY in this exact format:
SUBJECT: <one line, imperative mood, max 50 chars>
BODY:
<bullet points, each line max 72 chars>

Example:
SUBJECT: Add user authentication
BODY:
- Add login/logout endpoints
- Implement JWT token validation
- Add password hashing with bcrypt

Rules:
- Subject: imperative mood ("Add" not "Added"), max 50 chars
- Body: bullet points starting with "- ", explain WHY not WHAT
- Each line max 72 chars
- No preamble, just SUBJECT and BODY

Diff:
{diff}`

const defaultStackReviewPrompt = `Review this stack. Per-branch summary, then full stack summary.
{branches}`
