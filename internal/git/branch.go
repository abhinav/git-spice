package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"iter"
	"strings"
)

// LocalBranch represents a local branch in a repository.
type LocalBranch struct {
	// Name is the name of the branch.
	Name string

	// Worktree is the path at which this branch is checked out, if any.
	Worktree string
}

// LocalBranchesOptions specifies options for listing local branches.
type LocalBranchesOptions struct {
	// Sort specifies an optional value for git branch --sort.
	Sort string
}

// LocalBranches returns an iterator over local branches in the repository.
func (r *Repository) LocalBranches(ctx context.Context, opts *LocalBranchesOptions) iter.Seq2[LocalBranch, error] {
	if opts == nil {
		opts = &LocalBranchesOptions{}
	}

	args := []string{
		"for-each-ref", "--format=%(refname) %(worktreepath)",
	}
	if opts.Sort != "" {
		args = append(args, "--sort="+opts.Sort)
	}
	args = append(args, "refs/heads/")

	return func(yield func(LocalBranch, error) bool) {
		cmd := r.gitCmd(ctx, args...)
		for bs, err := range cmd.ScanLines(r.exec) {
			if err != nil {
				yield(LocalBranch{}, fmt.Errorf("git for-each-ref: %w", err))
				return
			}

			line := bytes.TrimSpace(bs)
			if len(line) == 0 {
				continue
			}

			refname, worktree, _ := bytes.Cut(line, []byte{' '})
			branchName, ok := bytes.CutPrefix(refname, []byte("refs/heads/"))
			if !ok {
				continue
			}

			localBranch := LocalBranch{
				Name:     string(branchName),
				Worktree: string(bytes.TrimSpace(worktree)),
			}
			if !yield(localBranch, nil) {
				return
			}
		}
	}
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

	// Force specifies that the branch should be created
	// at the given Head even if a branch with the same name
	// already exists.
	Force bool
}

// CreateBranch creates a new branch in the repository.
// This operation fails if a branch with the same name already exists.
func (r *Repository) CreateBranch(ctx context.Context, req CreateBranchRequest) error {
	r.log.Debug("Creating branch", "name", req.Name, "head", req.Head)

	args := []string{"branch", req.Name}
	if req.Force {
		args = append(args, "--force")
	}
	if req.Head != "" {
		args = append(args, req.Head)
	}
	if err := r.gitCmd(ctx, args...).Run(r.exec); err != nil {
		return fmt.Errorf("git branch: %w", err)
	}
	return nil
}

// BranchExists reports whether a branch with the given name exists.
func (r *Repository) BranchExists(ctx context.Context, branch string) bool {
	return r.gitCmd(ctx, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).
		Run(r.exec) == nil
}

// DetachHead detaches the HEAD from the current branch
// and points it to the specified commitish (if provided).
// Otherwise, it stays at the current commit.
func (r *Repository) DetachHead(ctx context.Context, commitish string) error {
	r.log.Debug("Detaching HEAD", "commit", commitish)

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
	r.log.Debug("Checking out branch", "name", branch)

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

	// Remote indicates that the branch being deleted
	// is a remote tracking branch.
	Remote bool
}

// DeleteBranch deletes a branch from the repository.
// It returns an error if the branch does not exist,
// or if it has unmerged changes and the Force option is not set.
func (r *Repository) DeleteBranch(
	ctx context.Context,
	branch string,
	opts BranchDeleteOptions,
) error {
	r.log.Debug("Deleting branch", "name", branch)

	args := []string{"branch", "--delete"}
	if opts.Force {
		args = append(args, "--force")
	}
	if opts.Remote {
		args = append(args, "--remotes")
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
	r.log.Debug("Renaming branch", "old", req.OldName, "new", req.NewName)

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
//
// If upstream is empty, the upstream is unset.
func (r *Repository) SetBranchUpstream(
	ctx context.Context,
	branch, upstream string,
) error {
	args := []string{"branch"}
	if upstream == "" {
		r.log.Debug("Unsetting branch upstream", "name", branch)
		args = append(args, "--unset-upstream")
	} else {
		r.log.Debug("Setting branch upstream", "name", branch, "upstream", upstream)
		args = append(args, "--set-upstream-to="+upstream)
	}
	args = append(args, branch)

	if err := r.gitCmd(ctx, args...).Run(r.exec); err != nil {
		return fmt.Errorf("git branch: %w", err)
	}
	return nil
}
