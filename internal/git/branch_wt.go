package git

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"strings"
)

// ErrDetachedHead indicates that the repository is
// unexpectedly in detached HEAD state.
var ErrDetachedHead = errors.New("in detached HEAD state")

// BranchInUseError indicates that a branch could not be checked out
// because it is already checked out in another worktree.
type BranchInUseError struct {
	Branch   string // branch that is in use
	Worktree string // path to the worktree that holds it
}

func (e *BranchInUseError) Error() string {
	return fmt.Sprintf("branch %v is in use by worktree %v", e.Branch, e.Worktree)
}

// CurrentBranch reports the current branch name.
// It returns [ErrDetachedHead] if the repository is in detached HEAD state.
func (w *Worktree) CurrentBranch(ctx context.Context) (string, error) {
	name, err := w.gitCmd(ctx, "branch", "--show-current").
		OutputChomp()
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

// BranchesAtHead reports local branches that point at the worktree's HEAD.
func (w *Worktree) BranchesAtHead(ctx context.Context) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		head, err := w.Head(ctx)
		if err != nil {
			yield("", fmt.Errorf("get HEAD: %w", err))
			return
		}

		for branch, err := range w.repo.BranchesAtCommitish(ctx, head.String()) {
			if !yield(branch, err) {
				return
			}
			if err != nil {
				return
			}
		}
	}
}

// DetachHead detaches the HEAD from the current branch
// and points it to the specified commitish (if provided).
// Otherwise, it stays at the current commit.
func (w *Worktree) DetachHead(ctx context.Context, commitish string) error {
	w.log.Debug("Detaching HEAD", "commit", commitish)

	args := []string{"--detach"}
	if len(commitish) > 0 {
		args = append(args, commitish)
	}

	if err := w.runGitWithIndexLockRetry(ctx, func() *gitCmd {
		return w.gitCmd(ctx, "checkout", args...)
	}); err != nil {
		return fmt.Errorf("git checkout: %w", err)
	}
	return nil
}

// CheckoutBranch switches to the specified branch.
// If the branch does not exist, it returns an error.
func (w *Worktree) CheckoutBranch(ctx context.Context, branch string) error {
	w.log.Debug("Checking out branch", "name", branch)

	// Git refuses to check out a branch already checked out in another
	// worktree. Detect that up front and return a legible error naming
	// the worktree, rather than git's raw 'already used by worktree' fatal.
	for wt, err := range w.repo.Worktrees(ctx) {
		if err != nil {
			return fmt.Errorf("list worktrees: %w", err)
		}
		if wt.Branch == branch && wt.Path != w.rootDir {
			return &BranchInUseError{Branch: branch, Worktree: wt.Path}
		}
	}

	if err := w.runGitWithIndexLockRetry(ctx, func() *gitCmd {
		return w.gitCmd(ctx, "checkout", branch)
	}); err != nil {
		return fmt.Errorf("git checkout: %w", err)
	}
	return nil
}
