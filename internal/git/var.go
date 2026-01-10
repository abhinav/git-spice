package git

import (
	"context"
	"fmt"
	"strings"
)

// Var returns the value of the given Git variable.
func (r *Repository) Var(ctx context.Context, name string) (string, error) {
	cmd := newGitCmd(ctx, r.log, r.exec, "var", name)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git var %s: %w", name, err)
	}
	return strings.TrimSpace(string(out)), nil
}
