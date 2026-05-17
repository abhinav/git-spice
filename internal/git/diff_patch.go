package git

import (
	"context"
	"fmt"
	"io"
)

// DiffTreePatch writes the patch output for the diff
// between two tree-ish references to w.
func (r *Repository) DiffTreePatch(ctx context.Context, w io.Writer, treeish1, treeish2 string) error {
	cmd := r.gitCmd(ctx, "diff-tree", "--patch", treeish1, treeish2).WithStdout(w)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("diff-tree: %w", err)
	}
	return nil
}
