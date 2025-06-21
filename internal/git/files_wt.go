package git

import (
	"cmp"
	"context"
	"fmt"
	"iter"
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
	args := []string{"ls-files", "--format=%(path)"}
	if opts.Unmerged {
		args = append(args, "--unmerged")
	}

	shown := make(map[string]struct{})
	return func(yield func(string, error) bool) {
		cmd := w.gitCmd(ctx, args...)
		for line, err := range cmd.ScanLines(w.exec) {
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
