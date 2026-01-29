package git

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/xec"
)

// RebaseInterruptKind specifies the kind of rebase interruption.
type RebaseInterruptKind int

const (
	// RebaseInterruptConflict indicates that a rebase operation
	// was interrupted due to a conflict.
	RebaseInterruptConflict RebaseInterruptKind = iota

	// RebaseInterruptDeliberate indicates that a rebase operation
	// was interrupted deliberately by the user.
	// This is usually done to edit the rebase instructions.
	RebaseInterruptDeliberate
)

// RebaseInterruptError indicates that a rebasing operation was interrupted.
// It includes the kind of interruption and the current rebase state.
type RebaseInterruptError struct {
	Kind  RebaseInterruptKind
	State *RebaseState // always non-nil

	// Err is non-nil only if the rebase operation failed
	// due to a conflict.
	Err error
}

func (e *RebaseInterruptError) Error() string {
	var msg strings.Builder
	msg.WriteString("rebase")
	if e.State != nil {
		fmt.Fprintf(&msg, " of %s", e.State.Branch)
	}
	msg.WriteString(" interrupted")
	switch e.Kind {
	case RebaseInterruptConflict:
		msg.WriteString(" by a conflict")
	case RebaseInterruptDeliberate:
		msg.WriteString(" deliberately")
	}
	if e.Err != nil {
		fmt.Fprintf(&msg, ": %v", e.Err)
	}
	return msg.String()
}

func (e *RebaseInterruptError) Unwrap() error {
	return e.Err
}

// RebaseRequest is a request to rebase a branch.
type RebaseRequest struct {
	// Branch is the branch to rebase.
	Branch string

	// Upstream is the upstream commitish
	// from which the current branch started.
	//
	// Commits between Upstream and Branch will be rebased.
	Upstream string

	// Onto is the new base commit to rebase onto.
	// If unspecified, defaults to Upstream.
	Onto string

	// Autostash is true if the rebase should automatically stash
	// dirty changes before starting the rebase operation,
	// and re-apply them after the rebase is complete.
	Autostash bool

	// Quiet reduces the output of the rebase operation.
	Quiet bool

	// Interactive is true if the rebase should present the user
	// with a list of rebase instructions to edit
	// before starting the rebase operation.
	Interactive bool
}

// Rebase runs a git rebase operation with the specified parameters.
// It returns [RebaseInterruptError] for known rebase interruptions,
func (w *Worktree) Rebase(ctx context.Context, req RebaseRequest) (err error) {
	args := []string{
		// Never include advice on how to resolve merge conflicts.
		// We'll do that ourselves.
		"-c", "advice.mergeConflict=false",
		"rebase",
	}
	if req.Interactive {
		args = append(args, "--interactive")
	}
	if req.Onto != "" {
		args = append(args, "--onto", req.Onto)
	}
	if req.Autostash {
		args = append(args, "--autostash")
		// If autosquash is enabled,
		// but the squash-pop failed,
		// git still exits with a zero exit code.
		// So we need to check if we're left with any unmerged files
		// separately and fail the operation if so.
		defer func() {
			if err != nil {
				return
			}

			var unmergedFiles []string
			for path := range w.ListFilesPaths(ctx, &ListFilesOptions{Unmerged: true}) {
				unmergedFiles = append(unmergedFiles, path)
			}
			if len(unmergedFiles) == 0 {
				return
			}
			sort.Strings(unmergedFiles)

			w.log.Error("Dirty changes in the worktree were stashed, but could not be re-applied.")
			w.log.Error("The following files were left unmerged:")
			for _, file := range unmergedFiles {
				w.log.Error("  - " + silog.MaybeQuote(file))
			}
			w.log.Error("Resolve the conflict and run 'git stash drop' to remove the stash entry.")
			w.log.Error("Or change to a branch where the stash can apply, and run 'git stash pop'.")

			err = fmt.Errorf("%v: dirty changes could not be re-applied", req.Branch)
		}()
	}
	if req.Quiet {
		args = append(args, "--quiet")
	}
	if req.Upstream != "" {
		args = append(args, req.Upstream)
	}
	if req.Branch != "" {
		args = append(args, req.Branch)
	}

	w.log.Debug("Rebasing branch",
		silog.NonZero("name", req.Branch),
		silog.NonZero("onto", req.Onto),
		silog.NonZero("upstream", req.Upstream),
	)

	cmd := w.gitCmd(ctx, args...).WithLogPrefix("git rebase")
	if req.Interactive {
		cmd.WithStdin(os.Stdin).WithStdout(os.Stdout).WithStderr(os.Stderr)
	}

	if err := cmd.Run(); err != nil {
		return w.handleRebaseError(ctx, err)
	}
	return w.handleRebaseFinish(ctx)
}

// RebaseContinueOptions holds options for the rebase operation.
type RebaseContinueOptions struct {
	// Editor specifies the editor to use for interactive rebases.
	// If empty, the default editor will be used.
	Editor string
}

// RebaseContinue continues an ongoing rebase operation.
func (w *Worktree) RebaseContinue(ctx context.Context, opts *RebaseContinueOptions) error {
	opts = cmp.Or(opts, &RebaseContinueOptions{})
	cmd := w.gitCmd(ctx, "rebase", "--continue").WithStdin(os.Stdin).WithStdout(os.Stdout)
	if opts.Editor != "" {
		cmd = (&extraConfig{Editor: opts.Editor}).WithArgs(cmd)
	}
	if err := cmd.Run(); err != nil {
		return w.handleRebaseError(ctx, err)
	}
	return w.handleRebaseFinish(ctx)
}

func (w *Worktree) handleRebaseError(ctx context.Context, err error) error {
	originalErr := err
	if exitErr := new(xec.ExitError); !errors.As(err, &exitErr) {
		return fmt.Errorf("rebase: %w", err)
	}

	// If the rebase operation actually ran, but failed,
	// we might be in the middle of a rebase operation.
	state, err := w.RebaseState(ctx)
	if err != nil {
		// Rebase probably failed for a different reason,
		// so no need to log the state read failure verbosely.
		w.log.Debug("Failed to read rebase state", "error", err)
		return originalErr
	}

	return &RebaseInterruptError{
		Err:   originalErr,
		Kind:  RebaseInterruptConflict,
		State: state,
	}
}

func (w *Worktree) handleRebaseFinish(ctx context.Context) error {
	// If we have rebase state after a successful return,
	// this was a deliberate break or edit.
	if state, err := w.RebaseState(ctx); err == nil {
		return &RebaseInterruptError{
			Kind:  RebaseInterruptDeliberate,
			State: state,
			// TODO: should we include stderr as an Error
		}
	}

	return nil
}

// RebaseAbort aborts an ongoing rebase operation.
func (w *Worktree) RebaseAbort(ctx context.Context) error {
	if err := w.gitCmd(ctx, "rebase", "--abort").Run(); err != nil {
		return fmt.Errorf("rebase abort: %w", err)
	}
	return nil
}

// RebaseBackend specifies the kind of rebase backend in use.
//
// See https://git-scm.com/docs/git-rebase#_behavioral_differences for details.
type RebaseBackend int

const (
	// RebaseBackendMerge refers to the "merge" backend.
	// It is the default backend used by Git,
	// and handles more corner cases better.
	RebaseBackendMerge RebaseBackend = iota

	// RebaseBackendApply refers to the "apply" backend.
	// It is rarely used and may be phased out in the future
	// if the merge backend gains all of its features.
	// It is enabled with the --apply flag.
	RebaseBackendApply
)

func (b RebaseBackend) String() string {
	switch b {
	case RebaseBackendMerge:
		return "merge"
	case RebaseBackendApply:
		return "apply"
	default:
		return "unknown"
	}
}

// RebaseState holds information about the current state of a rebase operation.
type RebaseState struct {
	// Branch is the branch being rebased.
	Branch string

	// Backend specifies which merge backend is being used.
	// Merge is the default.
	// Apply is rarely used and may be phased out in the future.
	Backend RebaseBackend
}

// ErrNoRebase indicates that a rebase is not in progress.
var ErrNoRebase = errors.New("no rebase in progress")

// RebaseState loads information about an ongoing rebase,
// or [ErrNoRebase] if no rebase is in progress.
func (w *Worktree) RebaseState(context.Context) (*RebaseState, error) {
	// Rebase state is stored inside .git/rebase-merge or .git/rebase-apply
	// depending on the backend in use.
	// See https://github.com/git/git/blob/d8ab1d464d07baa30e5a180eb33b3f9aa5c93adf/wt-status.c#L1711.
	//
	// Inside that directory, we care about the following files:
	//
	//   - head-name: full ref name of the branch being rebased (e.g. refs/heads/main)
	//
	// There's no Git porcelain command to directly get this information.
	for _, backend := range []RebaseBackend{RebaseBackendApply, RebaseBackendMerge} {
		stateDir := filepath.Join(w.gitDir, backend.stateDir())
		if _, err := os.Stat(stateDir); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("check %v: %w", backend, err)
		}

		head, err := os.ReadFile(filepath.Join(stateDir, "head-name"))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read %v head: %w", backend, err)
		}

		branchRef := strings.TrimSpace(string(head))
		state := &RebaseState{
			Branch:  strings.TrimPrefix(branchRef, "refs/heads/"),
			Backend: backend,
		}

		return state, nil
	}

	return nil, ErrNoRebase
}

// stateDir reports the directory inside the .git directory
// where rebase state is stored.
//
// See
// https://github.com/git/git/blob/d8ab1d464d07baa30e5a180eb33b3f9aa5c93adf/wt-status.c#L1711.
func (b RebaseBackend) stateDir() string {
	switch b {
	case RebaseBackendMerge:
		return "rebase-merge"
	case RebaseBackendApply:
		return "rebase-apply"
	default:
		must.Failf("unknown rebase backend: %v", b)
		return ""
	}
}

// RebaseEdit starts an interactive rebase that pauses at the given commit
// for editing. This is equivalent to changing "pick" to "edit" for that commit
// in the rebase todo list.
//
// The function returns a [RebaseInterruptError] with Kind [RebaseInterruptDeliberate]
// when the rebase successfully pauses at the target commit.
func (w *Worktree) RebaseEdit(ctx context.Context, commit Hash) error {
	// Use sed to change "pick <hash>" to "edit <hash>" for the target commit.
	// The short hash is used in the rebase todo file.
	shortHash := commit.Short()

	// The sequence editor command replaces "pick <hash>" with "edit <hash>".
	// Git passes the todo file path as an argument to the editor.
	// With sh -c, we need to use -- to separate the script from arguments,
	// then use $1 to reference the file path.
	seqEditor := fmt.Sprintf(
		`sh -c 'sed -i.bak "s/^pick %s/edit %s/" "$1"' --`,
		shortHash, shortHash,
	)

	args := []string{
		"-c", "sequence.editor=" + seqEditor,
		"rebase", "--interactive", commit.String() + "^",
	}

	cmd := w.gitCmd(ctx, args...).WithLogPrefix("git rebase")
	if err := cmd.Run(); err != nil {
		return w.handleRebaseError(ctx, err)
	}
	return w.handleRebaseFinish(ctx)
}
