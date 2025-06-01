package git

import (
	"context"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/silog"
)

// Worktree is a checkout of a Git repository at a specific path.
// Operations that require a working tree (e.g. branch checkout, rebase, etc.)
// are only available on the worktree.
type Worktree struct {
	gitDir  string // absolute path to wt's .git directory
	rootDir string // absolute path to the root directory of the worktree
	repo    *Repository

	log  *silog.Logger
	exec execer
}

func newWorktree(gitDir, rootDir string, repo *Repository, log *silog.Logger, exec execer) *Worktree {
	return &Worktree{
		gitDir:  gitDir,
		rootDir: rootDir,
		repo:    repo,
		log:     log,
		exec:    exec,
	}
}

// OpenWorktree opens a worktree of this repository at the given directory.
func (r *Repository) OpenWorktree(ctx context.Context, dir string) (*Worktree, error) {
	out, err := r.gitCmd(ctx, "rev-parse", "--show-toplevel", "--absolute-git-dir").
		Dir(dir).
		OutputString(r.exec)
	if err != nil {
		return nil, err
	}

	rootDir, gitDir, ok := strings.Cut(out, "\n")
	if !ok {
		return nil, fmt.Errorf("unexpected output from git rev-parse: %q", out)
	}
	return newWorktree(gitDir, rootDir, r, r.log, r.exec), nil
}

func (r *Worktree) gitCmd(ctx context.Context, args ...string) *gitCmd {
	return newGitCmd(ctx, r.log, args...).Dir(r.rootDir)
}

// Repository returns the Git repository that this worktree belongs to.
func (r *Worktree) Repository() *Repository {
	return r.repo
}
