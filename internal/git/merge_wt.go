package git

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/xec"
)

// MergeRequest is a request to merge a commit-ish into the current branch.
type MergeRequest struct {
	// Commit is the commit-ish to merge into the current branch.
	//
	// For a merge-based restack, this is the new base of the branch.
	Commit string // required

	// Message is the commit message for the merge commit.
	// If empty, Git's default merge message is used.
	Message string

	// NoEdit skips opening an editor for the merge commit message.
	NoEdit bool

	// StrategyOption is passed verbatim to "git merge -X <option>"
	// when non-empty.
	// For example, "ours" and "theirs" auto-resolve textual conflicts
	// in favor of the current branch or the merged-in commit
	// respectively.
	StrategyOption string

	// Rerere enables "reuse recorded resolution" for this merge only,
	// without writing rerere settings to user or repository config.
	Rerere bool

	// Quiet reduces the output of the merge operation.
	Quiet bool

	// Autostash stashes uncommitted changes before merging
	// and restores them afterwards, like "git merge --autostash".
	Autostash bool
}

// Merge merges the requested commit-ish into the current branch.
//
// It returns a [MergeInterruptError] if the merge is interrupted
// by a conflict that requires user intervention.
func (w *Worktree) Merge(ctx context.Context, req MergeRequest) error {
	var args []string
	if req.NoEdit {
		args = append(args, "--no-edit")
	}
	if req.Message != "" {
		args = append(args, "-m", req.Message)
	}
	if req.StrategyOption != "" {
		args = append(args, "-X", req.StrategyOption)
	}
	if req.Quiet {
		args = append(args, "-q")
	}
	if req.Autostash {
		args = append(args, "--autostash")
	}
	args = append(args, req.Commit)

	w.log.Debug("Merging into current branch",
		"commit", req.Commit,
		silog.NonZero("strategyOption", req.StrategyOption),
	)

	extraCfg := &extraConfig{
		// Never include advice on how to resolve merge conflicts.
		// We'll do that ourselves.
		AdviceMergeConflict: new(false),
	}
	if req.Rerere {
		extraCfg.RerereEnabled = new(true)
		extraCfg.RerereAutoUpdate = new(true)
	}

	if err := w.runGitWithIndexLockRetry(ctx, func() *gitCmd {
		return w.gitCmd(ctx, "merge", args...).
			WithExtraConfig(extraCfg)
	}); err != nil {
		return w.handleMergeError(ctx, err)
	}
	return nil
}

// MergeInterruptError indicates that a merge operation was interrupted,
// usually by a conflict that requires user intervention.
type MergeInterruptError struct {
	// State is the state of the interrupted merge.
	State *MergeState // always non-nil

	// Err is the underlying error from the merge command.
	Err error
}

func (e *MergeInterruptError) Error() string {
	var msg strings.Builder
	msg.WriteString("merge")
	if e.State != nil {
		fmt.Fprintf(&msg, " of %s", e.State.Branch)
	}
	msg.WriteString(" interrupted by a conflict")
	if e.Err != nil {
		fmt.Fprintf(&msg, ": %v", e.Err)
	}
	return msg.String()
}

func (e *MergeInterruptError) Unwrap() error {
	return e.Err
}

// InterruptedBranch reports the branch on which the merge was interrupted.
func (e *MergeInterruptError) InterruptedBranch() string {
	return e.State.Branch
}

func (*MergeInterruptError) interruptError() {}

var _ InterruptError = (*MergeInterruptError)(nil)

func (w *Worktree) handleMergeError(ctx context.Context, err error) error {
	if exitErr := new(xec.ExitError); !errors.As(err, &exitErr) {
		return fmt.Errorf("merge: %w", err)
	}

	// If the merge ran but failed, it may have left a conflict behind.
	state, stateErr := w.MergeState(ctx)
	if stateErr != nil {
		// No merge in progress: the command failed for another reason
		// (e.g. bad arguments), so surface the original error.
		if errors.Is(stateErr, ErrNoMerge) {
			return err
		}

		w.log.Debug("Failed to read merge state", "error", stateErr)
		return err
	}

	return &MergeInterruptError{State: state, Err: err}
}

// MergeContinueOptions holds options for continuing a merge operation.
type MergeContinueOptions struct {
	// Edit opens an editor to modify the merge commit message.
	// By default, the existing message is used as-is.
	Edit bool
}

// MergeContinue finalizes a merge that was interrupted by a conflict,
// after the conflict has been resolved and staged.
//
// It returns a [MergeInterruptError]
// if there are still unresolved conflicts.
func (w *Worktree) MergeContinue(ctx context.Context, opts *MergeContinueOptions) error {
	opts = cmp.Or(opts, &MergeContinueOptions{})

	// Finalize with "git commit", not "git merge --continue":
	// both create the merge commit, but committing directly reuses the
	// same index-lock retry and editor suppression as other writes.
	var args []string
	if !opts.Edit {
		args = append(args, "--no-edit")
	}

	if err := w.runGitWithIndexLockRetry(ctx, func() *gitCmd {
		cmd := w.gitCmd(ctx, "commit", args...).
			WithStdin(os.Stdin).
			WithStdout(os.Stdout).
			WithStderr(os.Stderr)
		if !opts.Edit {
			cmd = cmd.WithExtraConfig(&extraConfig{Editor: "true"})
		}
		return cmd
	}); err != nil {
		return w.handleMergeError(ctx, err)
	}
	return nil
}

// MergeAbort aborts an in-progress merge,
// restoring the branch to its pre-merge state.
func (w *Worktree) MergeAbort(ctx context.Context) error {
	if err := w.gitCmd(ctx, "merge", "--abort").Run(); err != nil {
		return fmt.Errorf("merge abort: %w", err)
	}
	return nil
}

// MergeState holds information about an in-progress merge.
type MergeState struct {
	// Branch is the branch the merge is being applied to.
	Branch string

	// Head is the commit-ish being merged in,
	// as recorded in MERGE_HEAD.
	Head Hash
}

// ErrNoMerge indicates that a merge is not in progress.
var ErrNoMerge = errors.New("no merge in progress")

// MergeState loads information about an in-progress merge,
// or [ErrNoMerge] if no merge is in progress.
//
// A merge is detected by the presence of .git/MERGE_HEAD.
func (w *Worktree) MergeState(ctx context.Context) (*MergeState, error) {
	// MERGE_HEAD exists only while a merge is in progress.
	mergeHead, err := os.ReadFile(filepath.Join(w.gitDir, "MERGE_HEAD"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoMerge
		}
		return nil, fmt.Errorf("read MERGE_HEAD: %w", err)
	}

	branch, err := w.CurrentBranch(ctx)
	if err != nil {
		return nil, fmt.Errorf("current branch: %w", err)
	}

	return &MergeState{
		Branch: branch,
		Head:   Hash(strings.TrimSpace(string(mergeHead))),
	}, nil
}
