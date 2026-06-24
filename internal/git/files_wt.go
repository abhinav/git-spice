package git

import (
	"cmp"
	"context"
	"fmt"
	"iter"

	"go.abhg.dev/gs/internal/scanutil"
)

// ListFilesOptions shows information about files
// in the worktree or the index.
type ListFilesOptions struct {
	// Unmerged states unmerged files should be shown in the output.
	Unmerged bool
}

// ListFilesPaths lists files in the worktree or the index
// using the given options to filter.
func (w *Worktree) ListFilesPaths(ctx context.Context, opts *ListFilesOptions) iter.Seq2[string, error] {
	opts = cmp.Or(opts, &ListFilesOptions{})
	args := []string{"-z", "--format=%(path)"}
	if opts.Unmerged {
		args = append(args, "--unmerged")
	}

	shown := make(map[string]struct{})
	return func(yield func(string, error) bool) {
		cmd := w.gitCmd(ctx, "ls-files", args...)
		for line, err := range cmd.Scan(scanutil.SplitNull) {
			if err != nil {
				yield("", fmt.Errorf("git ls-files: %w", err))
				continue
			}

			path := string(line)
			if _, ok := shown[path]; ok {
				// Skip duplicates
				continue
			}
			shown[path] = struct{}{}

			if !yield(path, nil) {
				return
			}
		}
	}
}

// ListUntrackedFiles lists untracked files in the worktree.
func (w *Worktree) ListUntrackedFiles(ctx context.Context) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		cmd := w.gitCmd(ctx, "ls-files", "-z", "--others", "--exclude-standard")
		for line, err := range cmd.Scan(scanutil.SplitNull) {
			if err != nil {
				yield("", fmt.Errorf("git ls-files: %w", err))
				continue
			}

			if !yield(string(line), nil) {
				return
			}
		}
	}
}

// IsClean reports whether the worktree has no uncommitted changes among
// tracked files. Staged changes, unstaged changes, and unmerged paths
// all make the worktree unclean. Untracked files are ignored.
func (w *Worktree) IsClean(ctx context.Context) (bool, error) {
	out, err := w.gitCmd(ctx, "status",
		"--porcelain", "--untracked-files=no").
		OutputChomp()
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	return out == "", nil
}
