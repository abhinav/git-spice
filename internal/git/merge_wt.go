package git

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/xec"
)

// MergeOptions specifies parameters for a [Worktree.Merge] operation.
type MergeOptions struct {
	// Refs lists the commits or branches to merge into HEAD.
	// At least one is required.
	Refs []string

	// NoFF forces a merge commit even when a fast-forward is possible.
	NoFF bool

	// Message is the commit message for the merge commit.
	// If empty, Git's default message is used.
	Message string

	// EnableRerere enables rerere.enabled and rerere.autoupdate
	// for this merge invocation only.
	// User git config is not modified.
	EnableRerere bool

	// LeaveConflict, when true, leaves a conflicting merge in the
	// worktree (with unmerged paths) rather than aborting it. The
	// caller is then responsible for resolving or aborting the merge.
	LeaveConflict bool
}

// MergeConflictError indicates that a [Worktree.Merge] could not be
// completed due to conflicts. By default the merge is aborted before
// this error is returned, leaving the worktree at HEAD. If
// [MergeOptions.LeaveConflict] is set, the worktree is left with
// unmerged paths.
type MergeConflictError struct {
	// Refs are the references that were being merged.
	Refs []string

	// ConflictPaths lists the files with unresolved conflicts.
	ConflictPaths []string
}

func (e *MergeConflictError) Error() string {
	switch len(e.Refs) {
	case 0:
		return fmt.Sprintf("merge conflict in %d file(s)", len(e.ConflictPaths))
	case 1:
		return fmt.Sprintf("merge of %s conflicted in %d file(s)",
			e.Refs[0], len(e.ConflictPaths))
	default:
		return fmt.Sprintf("merge of %s conflicted in %d file(s)",
			strings.Join(e.Refs, ", "), len(e.ConflictPaths))
	}
}

// Merge runs git merge with the given options.
//
// On a merge conflict, the merge is automatically aborted and a
// [*MergeConflictError] is returned with the list of conflicting paths.
func (w *Worktree) Merge(ctx context.Context, opts MergeOptions) error {
	if len(opts.Refs) == 0 {
		return errors.New("merge: at least one ref is required")
	}

	mergeArgs := []string{"merge"}
	if opts.NoFF {
		mergeArgs = append(mergeArgs, "--no-ff")
	}
	if opts.Message != "" {
		mergeArgs = append(mergeArgs, "-m", opts.Message)
	}
	mergeArgs = append(mergeArgs, opts.Refs...)

	cmd := w.gitCmd(ctx, "merge", mergeArgs[1:]...)
	if opts.EnableRerere {
		prefix := []string{
			"-c", "rerere.enabled=true",
			"-c", "rerere.autoupdate=true",
		}
		cmd = cmd.WithArgs(append(prefix, cmd.Args()...)...)
	}

	err := w.runGitWithIndexLockRetry(ctx, func() *gitCmd { return cmd })
	if err == nil {
		return nil
	}
	if !errors.As(err, new(*xec.ExitError)) {
		return fmt.Errorf("git merge: %w", err)
	}

	// A non-zero exit may mean conflicts. Check for unmerged files.
	unmerged, listErr := sliceutil.CollectErr(
		w.ListFilesPaths(ctx, &ListFilesOptions{Unmerged: true}))
	if listErr != nil {
		return fmt.Errorf("list unmerged files: %w", listErr)
	}
	if len(unmerged) == 0 {
		// rerere with autoupdate=true may have fully resolved and
		// staged the conflicts, leaving git merge with a non-zero exit
		// but no unmerged paths. In that case, commit the merge to
		// complete it. If no merge is in progress, surface the
		// original error.
		mergeMsg := opts.Message
		if mergeMsg == "" {
			mergeMsg = "Merge"
		}
		commitCmd := w.gitCmd(ctx, "commit", "--no-edit", "-m", mergeMsg)
		if commitErr := w.runGitWithIndexLockRetry(ctx, func() *gitCmd { return commitCmd }); commitErr != nil {
			return fmt.Errorf("git merge: %w", err)
		}
		return nil
	}

	if !opts.LeaveConflict {
		if abortErr := w.MergeAbort(ctx); abortErr != nil {
			return fmt.Errorf("git merge: conflicted and abort failed: %w", abortErr)
		}
	}
	return &MergeConflictError{
		Refs:          opts.Refs,
		ConflictPaths: unmerged,
	}
}

// MergeAbort aborts an ongoing merge operation.
func (w *Worktree) MergeAbort(ctx context.Context) error {
	if err := w.gitCmd(ctx, "merge", "--abort").Run(); err != nil {
		return fmt.Errorf("git merge --abort: %w", err)
	}
	return nil
}
