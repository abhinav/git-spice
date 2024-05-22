package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.abhg.dev/git-spice/internal/must"
)

// ErrRebaseInterrupted is returned when a rebase operation is interrupted
// because of a
var ErrRebaseInterrupted = errors.New("rebase interrupted")

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

	// InterruptFunc, if set, is called if a rebase operation
	// is interrupted because of a conflict,
	// or because the user an instruction to pause the rebase
	// (e.g. 'edit' or 'break').
	//
	// The Rebase function will return the error returned by this function.
	InterruptFunc func(context.Context, *RebaseState) error
}

// Rebase runs a git rebase operation with the specified parameters.
func (r *Repository) Rebase(ctx context.Context, req RebaseRequest) error {
	args := []string{"rebase"}
	if req.Interactive {
		args = append(args, "--interactive")
	}
	if req.Onto != "" {
		args = append(args, "--onto", req.Onto)
	}
	if req.Autostash {
		args = append(args, "--autostash")
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

	err := r.gitCmd(ctx, args...).Run(r.exec)
	if err != nil {
		originalErr := err
		if exitErr := new(exec.ExitError); !errors.As(err, &exitErr) {
			return fmt.Errorf("rebase: %w", err)
		}

		// If the rebase operation actually ran, but failed,
		// we might be in the middle of a rebase operation.
		state, err := r.loadRebaseState(false /* deliberate */)
		if err != nil {
			// Rebase probably failed for a different reason,
			// so no need to log the state read failure verbosely.
			r.log.Debug("Failed to read rebase state: %v", err)
			return originalErr
		}

		if req.InterruptFunc == nil {
			// The rebase failed, but we don't have a way to handle it.
			// Return ErrRebaseInterrupted.
			return errors.Join(ErrRebaseInterrupted, originalErr)
		}

		return req.InterruptFunc(ctx, state)
	}

	// If we have rebase state after a successful return,
	// this was a deliberate break or edit.
	if state, err := r.loadRebaseState(true /* deliberate */); err == nil {
		if req.InterruptFunc == nil {
			return ErrRebaseInterrupted
		}
		return req.InterruptFunc(ctx, state)
	}

	return nil
}

// RebaseAbort aborts an ongoing rebase operation.
func (r *Repository) RebaseAbort(ctx context.Context) error {
	if err := r.gitCmd(ctx, "rebase", "--abort").Run(r.exec); err != nil {
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

	// Deliberate is true if the rebase was interrupted
	// because of a deliberate user action (e.g. 'edit' or 'break').
	Deliberate bool
}

// loadRebaseState loads information about an ongoing rebase.
//
// Rebase state is stored inside .git/rebase-merge or .git/rebase-apply
// depending on the backend in use.
// See https://github.com/git/git/blob/d8ab1d464d07baa30e5a180eb33b3f9aa5c93adf/wt-status.c#L1711.
// Inside that directory, we care about the following files:
//
//   - head-name: full ref name of the branch being rebased (e.g. refs/heads/main)
//
// There's no Git porcelain command to directly get this information.
func (r *Repository) loadRebaseState(deliberate bool) (*RebaseState, error) {
	for _, backend := range []RebaseBackend{RebaseBackendApply, RebaseBackendMerge} {
		stateDir := filepath.Join(r.gitDir, backend.stateDir())
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
			Branch:     strings.TrimPrefix(branchRef, "refs/heads/"),
			Backend:    backend,
			Deliberate: deliberate,
		}

		return state, nil
	}

	return nil, errors.New("no rebase in progress")
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
