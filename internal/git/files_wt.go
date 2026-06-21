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

// StageFiles stages the given paths via `git add`. No-op when the
// path list is empty. Used by auto-resolve loops after a resolver
// script has rewritten conflicted files in place.
func (w *Worktree) StageFiles(ctx context.Context, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	addArgs := append([]string{"--"}, paths...)
	if err := w.gitCmd(ctx, "add", addArgs...).Run(); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	return nil
}
