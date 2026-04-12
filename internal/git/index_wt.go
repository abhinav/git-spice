package git

import (
	"context"
	"fmt"
	"strings"
)

// WriteIndexTree writes the current index to a new tree object.
func (w *Worktree) WriteIndexTree(ctx context.Context) (Hash, error) {
	var out strings.Builder
	err := w.runGitWithIndexLockRetry(ctx, func() *gitCmd {
		out.Reset()
		return w.gitCmd(ctx, "write-tree").WithStdout(&out)
	})
	if err != nil {
		return "", fmt.Errorf("write-tree: %w", err)
	}
	result := strings.TrimSuffix(out.String(), "\n")
	return Hash(result), nil
}
