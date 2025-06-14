package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrDetachedHead indicates that the repository is
// unexpectedly in detached HEAD state.
var ErrDetachedHead = errors.New("in detached HEAD state")

// CurrentBranch reports the current branch name.
// It returns [ErrDetachedHead] if the repository is in detached HEAD state.
func (w *Worktree) CurrentBranch(ctx context.Context) (string, error) {
	name, err := w.gitCmd(ctx, "branch", "--show-current").
		OutputString(w.exec)
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	name = strings.TrimSpace(name)
	if len(name) == 0 {
		// Per man git-rev-parse, --show-current returns an empty string
		// if the repository is in detached HEAD state.
		return "", ErrDetachedHead
	}
	return name, nil
}

// DetachHead detaches the HEAD from the current branch
// and points it to the specified commitish (if provided).
// Otherwise, it stays at the current commit.
func (w *Worktree) DetachHead(ctx context.Context, commitish string) error {
	w.log.Debug("Detaching HEAD", "commit", commitish)

	args := []string{"checkout", "--detach"}
	if len(commitish) > 0 {
		args = append(args, commitish)
	}
	if err := w.gitCmd(ctx, args...).Run(w.exec); err != nil {
		return fmt.Errorf("git checkout: %w", err)
	}
	return nil
}

// Checkout switches to the specified branch.
// If the branch does not exist, it returns an error.
func (w *Worktree) Checkout(ctx context.Context, branch string) error {
	w.log.Debug("Checking out branch", "name", branch)

	if err := w.gitCmd(ctx, "checkout", branch).Run(w.exec); err != nil {
		return fmt.Errorf("git checkout: %w", err)
	}
	return nil
}
