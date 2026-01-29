package main

// claudeCmd groups Claude AI integration commands.
type claudeCmd struct {
	Review claudeReviewCmd `cmd:"" help:"Review changes using Claude AI"`
}
