package git

import (
	"context"
	"errors"
	"fmt"
)

// ErrNoChanges is returned when there are no changes to stash.
var ErrNoChanges = errors.New("no changes to stash")

// StashCreate creates a stash entry and returns its object name.
// It does not store the stash in the stash reflog.
// Returns ErrNoChanges if there are no changes to stash.
func (w *Worktree) StashCreate(ctx context.Context, message string) (Hash, error) {
	args := []string{"stash", "create"}
	if message != "" {
		args = append(args, message)
	}

	out, err := w.gitCmd(ctx, args...).OutputString(w.exec)
	if err != nil {
		return ZeroHash, fmt.Errorf("stash create: %w", err)
	}

	if out == "" {
		return ZeroHash, ErrNoChanges
	}

	return Hash(out), nil
}

// StashStore stores a stash created by StashCreate in the stash reflog.
func (w *Worktree) StashStore(ctx context.Context, stashHash Hash, message string) error {
	args := []string{"stash", "store"}
	if message != "" {
		args = append(args, "-m", message)
	}
	args = append(args, stashHash.String())

	if err := w.gitCmd(ctx, args...).Run(w.exec); err != nil {
		return fmt.Errorf("stash store: %w", err)
	}

	return nil
}

// StashApply applies a stash to the working directory.
// If stash is not supplied, the most recent stash is applied.
// Unlike 'stash pop', this accepts a hash string to identify the stash.
func (w *Worktree) StashApply(ctx context.Context, stash string) error {
	args := []string{"stash", "apply"}
	if stash != "" {
		args = append(args, stash)
	}

	if err := w.gitCmd(ctx, args...).CaptureStdout().Run(w.exec); err != nil {
		return fmt.Errorf("stash apply: %w", err)
	}

	return nil
}
