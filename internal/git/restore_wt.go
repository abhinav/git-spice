package git

import (
	"context"
	"fmt"
)

// RestoreRequest specifies options for the git restore command.
type RestoreRequest struct {
	// PathSpecs specifies the files or directories to restore.
	// These may include patterns (e.g. "*.go").
	PathSpecs []string // required
}

// Restore restores files in the working tree and/or index from a tree-ish.
func (w *Worktree) Restore(ctx context.Context, req *RestoreRequest) error {
	args := []string{"restore"}
	args = append(args, "--")
	args = append(args, req.PathSpecs...)
	if err := w.gitCmd(ctx, args...).Run(w.exec); err != nil {
		return fmt.Errorf("git restore: %w", err)
	}

	return nil
}
