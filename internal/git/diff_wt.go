package git

import (
	"context"
	"fmt"
	"os"
)

// DiffBranch runs git diff between the given base and head
// using triple-dot syntax (base...head).
// Output is written directly to stdout and stderr,
// allowing the user to see the diff in their terminal.
func (w *Worktree) DiffBranch(ctx context.Context, base, head string) error {
	if err := w.gitCmd(ctx, "diff", base+"..."+head).
		WithStdout(os.Stdout).
		WithStderr(os.Stderr).
		Run(); err != nil {
		return fmt.Errorf("diff: %w", err)
	}
	return nil
}

// DiffBranchBytes returns the unified diff output
// between base and head using triple-dot syntax.
func (w *Worktree) DiffBranchBytes(
	ctx context.Context,
	base, head string,
) ([]byte, error) {
	out, err := w.gitCmd(
		ctx, "diff", base+"..."+head,
	).Output()
	if err != nil {
		return nil, fmt.Errorf("diff: %w", err)
	}
	return out, nil
}
