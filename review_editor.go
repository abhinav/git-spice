package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/xec"
)

// editReviewCommentBody opens the configured editor with initial contents.
func editReviewCommentBody(
	ctx context.Context,
	repo *git.Repository,
	initial string,
) (string, error) {
	tmpFile := filepath.Join(os.TempDir(), "GIT_SPICE_REVIEW_EDITMSG.md")
	if err := os.WriteFile(tmpFile, []byte(initial), 0o644); err != nil {
		return "", fmt.Errorf("write temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmpFile) }()

	cmd := xec.EditCommand(gitEditor(ctx, repo), tmpFile)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("run editor: %w", err)
	}

	content, err := os.ReadFile(tmpFile)
	if err != nil {
		return "", fmt.Errorf("read temp file: %w", err)
	}
	return string(content), nil
}
