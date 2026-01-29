package claude

import (
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/silog"
)

// PreparedDiff contains the result of preparing a diff for Claude.
type PreparedDiff struct {
	// FilteredDiff is the reconstructed diff after filtering.
	FilteredDiff string

	// Config is the loaded Claude configuration.
	Config *Config

	// Client is the Claude client ready for use.
	Client *Client
}

// PrepareDiffOptions configures how a diff is prepared.
type PrepareDiffOptions struct {
	// Log is the logger for warnings.
	Log *silog.Logger
}

// PrepareDiff prepares a diff for Claude by parsing, filtering,
// and checking the budget. Returns an error if the diff is empty,
// unparseable, or exceeds the budget.
//
// This consolidates common logic used by commit message generation
// and PR summary generation.
func PrepareDiff(diffText string, opts *PrepareDiffOptions) (*PreparedDiff, error) {
	if opts == nil {
		opts = &PrepareDiffOptions{}
	}
	log := opts.Log
	if log == nil {
		log = silog.Nop()
	}

	// Check Claude availability.
	client := NewClient(nil)
	if !client.IsAvailable() {
		return nil, errors.New("claude CLI not found; please install it")
	}

	// Load configuration.
	cfg, err := LoadConfig(DefaultConfigPath())
	if err != nil {
		log.Warn("Could not load claude config, using defaults", "error", err)
		cfg = DefaultConfig()
	}

	if diffText == "" {
		return nil, errors.New("no changes to process")
	}

	// Early size check to prevent OOM on huge diffs.
	// This is a rough check; the actual limit is based on line count after filtering.
	const maxRawDiffSize = 50 * 1024 * 1024 // 50 MB
	if len(diffText) > maxRawDiffSize {
		return nil, fmt.Errorf(
			"diff too large (%d MB); use --from/--to to narrow the range",
			len(diffText)/(1024*1024),
		)
	}

	// Parse the diff.
	files, err := ParseDiff(diffText)
	if err != nil {
		return nil, fmt.Errorf(
			"parse diff: %w (check for unusual file names or binary files)",
			err,
		)
	}

	// Filter out ignored files and binaries.
	filtered := FilterDiff(files, cfg.IgnorePatterns)
	if len(filtered) == 0 {
		return nil, errors.New("no changes after filtering")
	}

	// Check budget.
	budget := CheckBudget(filtered, cfg.MaxLines)
	if budget.OverBudget {
		return nil, fmt.Errorf(
			"diff too large (%d lines, max %d)",
			budget.TotalLines, budget.MaxLines,
		)
	}

	return &PreparedDiff{
		FilteredDiff: ReconstructDiff(filtered),
		Config:       cfg,
		Client:       client,
	}, nil
}

// RunClaudeError wraps common Claude client error handling.
// It converts known errors to user-friendly messages.
func RunClaudeError(err error) error {
	if errors.Is(err, ErrNotAuthenticated) {
		return errors.New("not authenticated with Claude; run 'claude auth' first")
	}
	if errors.Is(err, ErrRateLimited) {
		return errors.New("claude rate limit exceeded; try again later")
	}
	return err
}
