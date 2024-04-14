package git

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
)

// LocalBranches lists local branches in the repository.
func (r *Repository) LocalBranches(ctx context.Context) ([]string, error) {
	cmd := r.gitCmd(ctx, "branch")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("git branch: %w", err)
	}

	if err := cmd.Start(r.exec); err != nil {
		return nil, fmt.Errorf("start git branch: %w", err)
	}

	var branches []string
	scan := bufio.NewScanner(out)
	for scan.Scan() {
		line := bytes.TrimSpace(scan.Bytes())
		if len(line) == 0 {
			continue
		}

		switch line[0] {
		case '(':
			continue // (HEAD detached at ...)
		case '*', '+':
			// Current or checked out in another worktree.
			b := bytes.TrimSpace(line[1:])
			// TODO: instead of returning string,
			// return a list of LocalBranch objects
			// that also specify whether the branch is checked out.
			branches = append(branches, string(b))
		default:
			branches = append(branches, string(line))
		}
	}

	if err := scan.Err(); err != nil {
		return nil, fmt.Errorf("read output: %w", err)
	}

	if err := cmd.Wait(r.exec); err != nil {
		return nil, fmt.Errorf("git branch: %w", err)
	}

	return branches, nil
}

// ErrDetachedHead indicates that the repository is
// unexpectedly in detached HEAD state.
var ErrDetachedHead = errors.New("in detached HEAD state")

// CurrentBranch reports the current branch name.
// It returns [ErrDetachedHead] if the repository is in detached HEAD state.
func (r *Repository) CurrentBranch(ctx context.Context) (string, error) {
	name, err := r.gitCmd(ctx, "branch", "--show-current").
		OutputString(r.exec)
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

// CreateBranchRequest specifies the parameters for creating a new branch.
type CreateBranchRequest struct {
	// Name of the branch.
	Name string

	// Head is the commitish to start the branch from.
	// Defaults to the current HEAD.
	Head string
}

// CreateBranch creates a new branch in the repository.
// This operation fails if a branch with the same name already exists.
func (r *Repository) CreateBranch(ctx context.Context, req CreateBranchRequest) error {
	args := []string{"branch", req.Name}
	if req.Head != "" {
		args = append(args, req.Head)
	}
	if err := r.gitCmd(ctx, args...).Run(r.exec); err != nil {
		return fmt.Errorf("git branch: %w", err)
	}
	return nil
}

// DetachHead detaches the HEAD from the current branch
// while staying at the same commit.
func (r *Repository) DetachHead(ctx context.Context, commitish string) error {
	args := []string{"checkout", "--detach"}
	if len(commitish) > 0 {
		args = append(args, commitish)
	}
	if err := r.gitCmd(ctx, args...).Run(r.exec); err != nil {
		return fmt.Errorf("git checkout: %w", err)
	}
	return nil
}

// Checkout switches to the specified branch.
// If the branch does not exist, it returns an error.
func (r *Repository) Checkout(ctx context.Context, branch string) error {
	if err := r.gitCmd(ctx, "checkout", branch).Run(r.exec); err != nil {
		return fmt.Errorf("git checkout: %w", err)
	}
	return nil
}

// BranchDeleteOptions specifies options for deleting a branch.
type BranchDeleteOptions struct {
	// Force specifies that a branch should be deleted
	// even if it has unmerged changes.
	Force bool
}

// DeleteBranch deletes a branch from the repository.
// It returns an error if the branch does not exist,
// or if it has unmerged changes and the Force option is not set.
func (r *Repository) DeleteBranch(
	ctx context.Context,
	branch string,
	opts BranchDeleteOptions,
) error {
	args := []string{"branch", "--delete"}
	if opts.Force {
		args = append(args, "--force")
	}
	args = append(args, branch)

	if err := r.gitCmd(ctx, args...).Run(r.exec); err != nil {
		return fmt.Errorf("git branch: %w", err)
	}
	return nil
}

// RenameBranchRequest specifies the parameters for renaming a branch.
type RenameBranchRequest struct {
	// OldName is the current name of the branch.
	OldName string

	// NewName is the new name for the branch.
	NewName string
}

// RenameBranch renames a branch in the repository.
func (r *Repository) RenameBranch(ctx context.Context, req RenameBranchRequest) error {
	args := []string{"branch", "--move", req.OldName, req.NewName}
	if err := r.gitCmd(ctx, args...).Run(r.exec); err != nil {
		return fmt.Errorf("git branch: %w", err)
	}
	return nil
}

// BranchUpstream reports the upstream branch of a local branch.
// Returns [ErrNotExist] if the branch has no upstream configured.
func (r *Repository) BranchUpstream(ctx context.Context, branch string) (string, error) {
	upstream, err := r.gitCmd(ctx,
		"rev-parse",
		"--abbrev-ref",
		"--verify",
		"--quiet",
		"--end-of-options",
		branch+"@{upstream}",
	).OutputString(r.exec)
	if err != nil {
		return "", ErrNotExist
	}
	return upstream, nil
}

// SetBranchUpstream sets the upstream ref for a local branch.
// The upstream must be in the form "remote/branch".
func (r *Repository) SetBranchUpstream(
	ctx context.Context,
	branch, upstream string,
) error {
	if err := r.gitCmd(ctx,
		"branch",
		"--set-upstream-to="+upstream,
		branch,
	).Run(r.exec); err != nil {
		return fmt.Errorf("git branch: %w", err)
	}
	return nil
}
