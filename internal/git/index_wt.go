package git

import (
	"context"
	"fmt"
)

// WriteIndexTree writes the current index to a new tree object.
func (w *Worktree) WriteIndexTree(ctx context.Context) (Hash, error) {
	cmd := w.gitCmd(ctx, "write-tree")
	out, err := cmd.OutputChomp()
	if err != nil {
		return "", fmt.Errorf("write-tree: %w", err)
	}
	return Hash(out), nil
}
