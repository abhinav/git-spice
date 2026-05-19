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

	if err := w.runGitWithIndexLockRetry(ctx, func() *gitCmd {
		return w.gitCmd(ctx, "checkout", branch)
	}); err != nil {
		return fmt.Errorf("git checkout: %w", err)
	}
	return nil
}

// CheckoutNewBranchRequest is a request to create and switch to a new
// branch in one operation.
type CheckoutNewBranchRequest struct {
	// Name is the name of the branch to create.
	Name string // required

	// StartPoint is the commit-ish that the new branch should point at.
	// If empty, the current HEAD is used.
	StartPoint string

	// Force resets the branch if it already exists.
	// Without Force, the operation fails if the branch already exists.
	Force bool
}

// CheckoutNewBranch creates a new branch and switches to it.
// With Force=true, an existing branch with the same name is reset
// to StartPoint.
func (w *Worktree) CheckoutNewBranch(ctx context.Context, req CheckoutNewBranchRequest) error {
	if req.Name == "" {
		return errors.New("checkout new branch: name is required")
	}

	w.log.Debug("Creating and checking out new branch",
		"name", req.Name,
		"start", req.StartPoint,
		"force", req.Force,
	)

	flag := "-b"
	if req.Force {
		flag = "-B"
	}
	args := []string{flag, req.Name}
	if req.StartPoint != "" {
		args = append(args, req.StartPoint)
	}

	if err := w.runGitWithIndexLockRetry(ctx, func() *gitCmd {
		return w.gitCmd(ctx, "checkout", args...)
	}); err != nil {
		return fmt.Errorf("git checkout: %w", err)
	}
	return nil
}
