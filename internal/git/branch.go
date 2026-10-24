package git

import (
	"bytes"
	"context"
	"fmt"
	"iter"
	"strings"
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

	// Patterns filters branches by literal name or slash-delimited prefix.
	// For example, "feature" and "feature/" match "feature/topic", not "feature2".
	// A branch is returned if it matches any pattern; an empty pattern matches all.
	// Glob patterns are not supported.
	Patterns []string
}

// LocalBranches returns an iterator over local branches in the repository.
// If pattern arguments exceed the platform limit, it filters the listing locally.
func (r *Repository) LocalBranches(ctx context.Context, opts *LocalBranchesOptions) iter.Seq2[LocalBranch, error] {
	if opts == nil {
		opts = &LocalBranchesOptions{}
	}

	branches := r.queryLocalBranches(ctx, opts)
	return func(yield func(LocalBranch, error) bool) {
		var yielded bool
		var listErr error
		for branch, err := range branches {
			if err != nil {
				listErr = err
				break
			}
			yielded = true
			if !yield(branch, nil) {
				return
			}
		}
		if listErr == nil {
			return
		}
		if yielded || len(opts.Patterns) == 0 || !isArgListTooLong(listErr) {
			yield(LocalBranch{}, listErr)
			return
		}

		// Retry once without native filters. The query iterator does not retry,
		// so errors from this fallback cannot re-enter the recovery path.
		prefixes := make(branchPrefixFilter, len(opts.Patterns))
		for _, pattern := range opts.Patterns {
			prefixes.Put(pattern)
		}
		for branch, err := range r.queryLocalBranches(ctx, &LocalBranchesOptions{Sort: opts.Sort}) {
			if err != nil {
				yield(LocalBranch{}, err)
				return
			}
			if !prefixes.Matches(branch.Name) {
				continue
			}
			if !yield(branch, nil) {
				return
			}
		}
	}
}

// queryLocalBranches runs one native listing and resolves its worktree paths.
// opts must be non-nil. Retry and local filtering belong to LocalBranches.
func (r *Repository) queryLocalBranches(ctx context.Context, opts *LocalBranchesOptions) iter.Seq2[LocalBranch, error] {
	args := []string{"--format=%(refname) %(objectname) %(worktreepath)"}
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
		cmd := r.gitCmd(ctx, "for-each-ref", args...)
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
			// Git derives %(worktreepath) by stripping a trailing /.git from
			// the common Git directory. For a submodule, this leaves the
			// Git directory itself instead of the checkout:
			//
			//     super/
			//     ├── .git/modules/sub/  ← reported worktree path
			//     │   └── config        core.worktree = ../../../sub
			//     └── sub/              ← actual checkout
			//         └── .git          file: gitdir: ../.git/modules/sub
			//
			// When the reported path equals r.gitDir, use rev-parse via
			// OpenWorktree to resolve the checkout through core.worktree.
			// Bare repositories have no checkout, so keep their paths unchanged.
			if localBranch.Worktree == r.gitDir {
				bare, err := r.gitCmd(ctx, "rev-parse", "--is-bare-repository").OutputChomp()
				if err != nil {
					yield(LocalBranch{}, fmt.Errorf("check bare repository: %w", err))
					return
				}
				if bare == "false" {
					wt, err := r.OpenWorktree(ctx, r.gitDir)
					if err != nil {
						yield(LocalBranch{}, fmt.Errorf("resolve worktree path: %w", err))
						return
					}
					localBranch.Worktree = wt.RootDir()
				}
			}
			if !yield(localBranch, nil) {
				return
			}
		}
	}
}

// branchPrefixFilter matches literal branch names and slash-delimited prefixes.
// An empty prefix matches every branch.
type branchPrefixFilter map[string]struct{}

// Put adds a prefix, ignoring an optional trailing slash.
func (f branchPrefixFilter) Put(prefix string) {
	f[strings.TrimSuffix(prefix, "/")] = struct{}{}
}

func (f branchPrefixFilter) Matches(name string) bool {
	if _, ok := f[""]; ok {
		return true
	}
	for {
		if _, ok := f[name]; ok {
			return true
		}
		parent, _, ok := strings.CutLast(name, "/")
		if !ok {
			return false
		}
		name = parent
	}
}

// BranchesAtCommitish reports local branches that point at commitish.
func (r *Repository) BranchesAtCommitish(
	ctx context.Context,
	commitish string,
) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		cmd := r.gitCmd(
			ctx,
			"for-each-ref",
			"--format=%(refname:short)",
			"--points-at", commitish,
			"refs/heads/",
		)
		for line, err := range cmd.Lines() {
			if err != nil {
				yield("", fmt.Errorf("git for-each-ref: %w", err))
				return
			}

			name := strings.TrimSpace(string(line))
			if name == "" {
				continue
			}
			if !yield(name, nil) {
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

	args := []string{req.Name}
	if req.Force {
		args = append(args, "--force")
	}
	if req.Head != "" {
		args = append(args, req.Head)
	}
	if err := r.gitCmd(ctx, "branch", args...).Run(); err != nil {
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

	args := []string{"--delete"}
	if opts.Force {
		args = append(args, "--force")
	}
	if opts.Remote {
		args = append(args, "--remotes")
	}
	args = append(args, branch)

	if err := r.gitCmd(ctx, "branch", args...).Run(); err != nil {
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

	args := []string{"--move", req.OldName, req.NewName}
	if err := r.gitCmd(ctx, "branch", args...).Run(); err != nil {
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
	var args []string
	if upstream == "" {
		r.log.Debug("Unsetting branch upstream", "name", branch)
		args = append(args, "--unset-upstream")
	} else {
		r.log.Debug("Setting branch upstream", "name", branch, "upstream", upstream)
		args = append(args, "--set-upstream-to="+upstream)
	}
	args = append(args, branch)

	if err := r.gitCmd(ctx, "branch", args...).Run(); err != nil {
		return fmt.Errorf("git branch: %w", err)
	}
	return nil
}
