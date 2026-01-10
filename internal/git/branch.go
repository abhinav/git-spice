package git

import (
	"bytes"
	"context"
	"fmt"
	"iter"
)

// LocalBranch represents a local branch in a repository.
type LocalBranch struct {
	// Name is the name of the branch.
	Name string

	// Hash is the commit hash that the branch
	// currently points to.
	Hash Hash

	// Worktree is the path at which this branch is checked out, if any.
	Worktree string
}

// LocalBranchesOptions specifies options for listing local branches.
type LocalBranchesOptions struct {
	// Sort specifies an optional value for git branch --sort.
	Sort string

	// Patterns is a list of patterns to filter branches.
	// If specified, only branches matching at least one of the patterns
	// will be returned.
	//
	// Patterns may be:
	//
	//   - literal branch names, e.g. "main", will be matched exactly
	//   - branch prefixes ending in '/', e.g. "feature/",
	//     will match branches that start with "feature/"
	//   - glob patterns, e.g. 'foo*',
	//     will return branches that match the glob pattern
	//     in the same '/'-section.
	Patterns []string
}

// LocalBranches returns an iterator over local branches in the repository.
func (r *Repository) LocalBranches(ctx context.Context, opts *LocalBranchesOptions) iter.Seq2[LocalBranch, error] {
	if opts == nil {
		opts = &LocalBranchesOptions{}
	}

	args := []string{
		"for-each-ref", "--format=%(refname) %(objectname) %(worktreepath)",
	}
	if opts.Sort != "" {
		args = append(args, "--sort="+opts.Sort)
	}
	if len(opts.Patterns) > 0 {
		for _, pattern := range opts.Patterns {
			args = append(args, "refs/heads/"+pattern)
		}
	} else {
		args = append(args, "refs/heads/")
	}

	return func(yield func(LocalBranch, error) bool) {
		cmd := r.gitCmd(ctx, args...)
		for bs, err := range cmd.Lines() {
			if err != nil {
				yield(LocalBranch{}, fmt.Errorf("git for-each-ref: %w", err))
				return
			}

			line := bytes.TrimSpace(bs)
			if len(line) == 0 {
				continue
			}

			refname, line, ok := bytes.Cut(line, []byte{' '})
			if !ok {
				continue
			}
			hash, worktree, _ := bytes.Cut(line, []byte{' '})

			branchName, ok := bytes.CutPrefix(refname, []byte("refs/heads/"))
			if !ok {
				continue
			}

			localBranch := LocalBranch{
				Name:     string(branchName),
				Hash:     Hash(hash),
				Worktree: string(bytes.TrimSpace(worktree)),
			}
			if !yield(localBranch, nil) {
				return
			}
		}
	}
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
	if err := r.gitCmd(ctx, args...).Run(); err != nil {
		return fmt.Errorf("git branch: %w", err)
	}
	return nil
}

// BranchExists reports whether a branch with the given name exists.
func (r *Repository) BranchExists(ctx context.Context, branch string) bool {
	return r.gitCmd(ctx, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).
		Run() == nil
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

	if err := r.gitCmd(ctx, args...).Run(); err != nil {
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
	if err := r.gitCmd(ctx, args...).Run(); err != nil {
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
	).OutputChomp()
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

	if err := r.gitCmd(ctx, args...).Run(); err != nil {
		return fmt.Errorf("git branch: %w", err)
	}
	return nil
}
