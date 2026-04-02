package git

import (
	"context"
	"fmt"
	"strings"
)

// Var returns the value of the given Git variable
// as resolved from this worktree.
func (w *Worktree) Var(ctx context.Context, name string) (string, error) {
	out, err := w.gitCmd(ctx, "var", name).Output()
	if err != nil {
		return "", fmt.Errorf("git var %s: %w", name, err)
	}
	return strings.TrimSpace(string(out)), nil
}
