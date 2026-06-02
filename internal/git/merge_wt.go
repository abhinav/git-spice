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

	// EnableRerere forces rerere's enabled state for this merge
	// invocation only. User git config is not modified.
	//
	// When true: rerere.enabled and rerere.autoupdate are passed as
	// -c overrides so rerere replays cached resolutions and
	// auto-stages them after replay.
	//
	// When false: rerere.enabled=false is passed as a -c override.
	// This is a force-off, not a "leave alone" — otherwise a user-
	// level rerere.enabled=true would keep rerere replaying cached
	// resolutions even when the caller explicitly asked for it off
	// (e.g., 'gs integration rebuild --no-rerere').
	EnableRerere bool

	// LeaveConflict, when true, leaves a conflicting merge in the
	// worktree (with unmerged paths) rather than aborting it. The
	// caller is then responsible for resolving or aborting the merge.
	LeaveConflict bool

	// Env lists additional environment variables to set on the git
	// merge process. Each entry is "KEY=VALUE". These are inherited
	// by any merge drivers git invokes. Useful for passing per-merge
	// state to a custom driver (e.g., a log file path).
	Env []string

	// OnRerereReplay, if non-nil, is called once for each path whose
	// conflict was silently resolved from the rr-cache during this
	// merge. The path is taken from git's stderr line
	// "Resolved 'PATH' using previous resolution." Useful for
	// surfacing that an otherwise-clean merge actually replayed a
	// cached resolution — silent replays of stale entries are a
	// known footgun.
	OnRerereReplay func(path string)
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
	// EnableRerere is a force, not a hint. When true, override user
	// config to enable rerere AND autoupdate. When false, override to
	// disable — otherwise a user-level rerere.enabled=true would keep
	// rerere replaying cached resolutions even when the caller
	// explicitly asked for it off.
	var rerereCfg []string
	if opts.EnableRerere {
		rerereCfg = []string{
			"-c", "rerere.enabled=true",
			"-c", "rerere.autoupdate=true",
		}
	} else {
		rerereCfg = []string{
			"-c", "rerere.enabled=false",
		}
	}
	cmd = cmd.WithArgs(append(rerereCfg, cmd.Args()...)...)
	if len(opts.Env) > 0 {
		cmd = cmd.AppendEnv(opts.Env...)
	}
	if opts.OnRerereReplay != nil {
		cmd.cmd.TeeStderr(&rerereReplayObserver{cb: opts.OnRerereReplay})
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

// MergeContinue stages the listed paths and commits an in-progress
// merge. Used after an external resolver has modified the conflicting
// files. Returns an error if any unmerged paths remain after staging.
//
// message is used as the merge commit message.
func (w *Worktree) MergeContinue(
	ctx context.Context, paths []string, message string,
) error {
	if len(paths) > 0 {
		addArgs := append([]string{"--"}, paths...)
		if err := w.gitCmd(ctx, "add", addArgs...).Run(); err != nil {
			return fmt.Errorf("git add: %w", err)
		}
	}

	unmerged, err := sliceutil.CollectErr(
		w.ListFilesPaths(ctx, &ListFilesOptions{Unmerged: true}))
	if err != nil {
		return fmt.Errorf("list unmerged files: %w", err)
	}
	if len(unmerged) > 0 {
		return fmt.Errorf("unmerged paths remain after resolution: %s",
			strings.Join(unmerged, ", "))
	}

	if message == "" {
		message = "Merge"
	}
	if err := w.gitCmd(ctx, "commit", "--no-edit", "-m", message).Run(); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

// CheckoutTheirs replaces the given paths in the worktree with their
// "theirs" (MERGE_HEAD) version. Intended as a final-stage fallback
// during integration rebuild: after merge drivers and an optional
// resolver have run, any paths still conflicting fall through to this
// take-incoming step so the merge can complete.
//
// The caller is responsible for staging the paths and committing.
// See [Worktree.MergeContinue].
func (w *Worktree) CheckoutTheirs(ctx context.Context, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	args := append([]string{"--theirs", "--"}, paths...)
	if err := w.gitCmd(ctx, "checkout", args...).Run(); err != nil {
		return fmt.Errorf("git checkout --theirs: %w", err)
	}
	return nil
}

// AmendCommitAll stages all worktree changes (additions, modifications,
// deletions) and amends HEAD with --no-edit. Preserves merge-commit
// parentage.
//
// Used after a post-merge step (e.g., a regenerator) writes additional
// content that should be folded into the just-made commit, rather than
// added as a separate commit on top.
func (w *Worktree) AmendCommitAll(ctx context.Context) error {
	if err := w.gitCmd(ctx, "add", "-A").Run(); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if err := w.gitCmd(ctx,
		"commit", "--amend", "--no-edit", "--allow-empty",
	).Run(); err != nil {
		return fmt.Errorf("git commit --amend: %w", err)
	}
	return nil
}
