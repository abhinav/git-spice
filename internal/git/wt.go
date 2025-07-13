package git

import (
	"bytes"
	"context"
	"fmt"
	"iter"
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

func (w *Worktree) gitCmd(ctx context.Context, args ...string) *gitCmd {
	return newGitCmd(ctx, w.log, args...).Dir(w.rootDir)
}

// RootDir returns the absolute path to the root directory of the worktree.
func (w *Worktree) RootDir() string {
	return w.rootDir
}

// Repository returns the Git repository that this worktree belongs to.
func (w *Worktree) Repository() *Repository {
	return w.repo
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

// WorktreeListItem represents a worktree associated with a repository.
type WorktreeListItem struct {
	// Path is the path to the worktree.
	// Use this with Repository.OpenWorktree.
	Path string

	// Bare reports that the worktree is a bare repository.
	Bare bool

	// Detached reports that the worktree is in a detached HEAD state.
	Detached bool

	// LockedReason reports why the worktree is locked, if it is.
	// It is empty if the worktree is not locked.
	LockedReason string

	// Branch is the name of the branch checked out in this worktree.
	// If empty, the worktree may not have a branch checked out.
	Branch string

	// Head is the hash of the HEAD commit in this worktree.
	Head Hash
}

// Worktrees returns a list of worktrees associated with the repository.
func (r *Repository) Worktrees(ctx context.Context) iter.Seq2[*WorktreeListItem, error] {
	return func(yield func(*WorktreeListItem, error) bool) {
		var item *WorktreeListItem
		for line, err := range r.gitCmd(ctx, "worktree", "list", "--porcelain", "-z").Scan(r.exec, splitNullByte) {
			if err != nil {
				yield(nil, fmt.Errorf("worktree list: %w", err))
				return
			}

			// worktree list porcelain has output in the form:
			//
			//	worktree <path>
			//	attr1 <value>
			//	attr2 <value>
			//	boolattr1
			//	boolattr2
			//
			// Where worktree is the first line for a worktree,
			// and then the attributes follow.
			// An empty line indicates the end of a worktree entry.
			if len(line) == 0 {
				if item != nil {
					if !yield(item, nil) {
						return
					}
				}
				item = nil
				continue
			}

			key, value, _ := bytes.Cut(line, []byte(" "))
			switch string(key) {
			case "worktree":
				item = &WorktreeListItem{Path: string(value)}
			case "detached":
				item.Detached = true
			case "bare":
				item.Bare = true
			case "branch":
				item.Branch = strings.TrimPrefix(string(value), "refs/heads/")
			case "HEAD":
				item.Head = Hash(value)
			case "locked":
				item.LockedReason = string(value)
			default:
				// Ignore unknown attributes.
			}
		}
	}
}
