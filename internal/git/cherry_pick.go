package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// CherryPickInterruptedError indicates that a cherry-pick
// could not be applied successfully because of conflicts
// or because it would introduce an empty change.
//
// Once these conflicts are resolved, the cherry-pick can be continued.
type CherryPickInterruptedError struct {
	// Commit is the hash of the commit that could not be applied.
	Commit Hash

	// Err is the original error that was reported.
	Err error
}

func (e *CherryPickInterruptedError) Error() string {
	return fmt.Sprintf("cherry-pick %v interrupted", e.Commit)
}

func (e *CherryPickInterruptedError) Unwrap() error {
	return e.Err
}

// CherryPickEmpty specifies how to handle cherry-picked commits
// that would result in no changes to the current HEAD.
type CherryPickEmpty int

const (
	// CherryPickEmptyStop stops the cherry-pick operation
	// if a commit would have no effect on the current HEAD.
	// The user must resolve the issue, and then continue the operation.
	//
	// This is the default.
	CherryPickEmptyStop CherryPickEmpty = iota

	// CherryPickEmptyKeep keeps empty commits and their messages.
	//
	// AllowEmpty is assumed true if this is used.
	CherryPickEmptyKeep

	// CherryPickEmptyDrop ignores commits that have no effect
	// on the current HEAD.
	CherryPickEmptyDrop
)

// CherryPickRequest is a request to cherry-pick one or more commits
// into the current HEAD.
type CherryPickRequest struct {
	// Commits to cherry-pick. Must be non-empty.
	Commits []Hash

	// Edit allows editing the commit message(s)
	// before committing to the current HEAD.
	Edit bool

	// OnEmpty indicates how to handle empty cherry-picks:
	// those that would have no effect on the current tree.
	OnEmpty CherryPickEmpty

	// AllowEmpty enables cherry-picking of empty commits.
	// Without this, cherry-pick will fail if the target commit is empty
	// (has the same tree hash as its parent).
	//
	// Commits that are empty after merging into current tree
	// are not covered by this option.
	AllowEmpty bool
}

// CherryPick cherry-picks one or more commits into the current HEAD.
//
// Returns [CherryPickInterruptedError] if a commit could not be applied cleanly.
func (r *Repository) CherryPick(ctx context.Context, req CherryPickRequest) error {
	if len(req.Commits) == 0 {
		return errors.New("no commits specified")
	}

	args := []string{"cherry-pick"}
	if req.Edit {
		args = append(args, "--edit")
	}
	if req.AllowEmpty {
		args = append(args, "--allow-empty")
	}
	switch req.OnEmpty {
	case CherryPickEmptyStop:
		// default; do nothing
	case CherryPickEmptyKeep:
		args = append(args, "--empty=keep")
	case CherryPickEmptyDrop:
		args = append(args, "--empty=drop")
	default:
		return fmt.Errorf("unkonwn OnEmpty: %v", req.OnEmpty)
	}

	cmd := r.gitCmd(ctx, args...)
	if req.Edit {
		cmd.Stdin(os.Stdin).Stdout(os.Stdout)
	}

	return r.handleCherryPickError(ctx, "cherry-pick", cmd.Run(r.exec))
}

// CherryPickContinue continues a series of cherry-pick operations.
//
// Returns [CherryPickInterruptedError] if a commit could not be applied cleanly.
func (r *Repository) CherryPickContinue(ctx context.Context) error {
	cmd := r.gitCmd(ctx, "cherry-pick", "--continue").Stdin(os.Stdin).Stdout(os.Stdout)
	return r.handleCherryPickError(ctx, "cherry-pick continue", cmd.Run(r.exec))
}

// CherryPickSkip skips the current commit in a cherry-pick operation
// and continues the remaining ones.
//
// Returns [CherryPickInterruptedError] if a commit could not be applied cleanly.
func (r *Repository) CherryPickSkip(ctx context.Context) error {
	cmd := r.gitCmd(ctx, "cherry-pick", "--skip").Stdin(os.Stdin).Stdout(os.Stdout)
	return r.handleCherryPickError(ctx, "cherry-pick skip", cmd.Run(r.exec))
}

// CherryPickAbort aborts the current cherry-pick operations
// and reverts to the state before the cherry-pick.
func (r *Repository) CherryPickAbort(ctx context.Context) error {
	cmd := r.gitCmd(ctx, "cherry-pick", "--abort").Stdin(os.Stdin).Stdout(os.Stdout)
	if err := cmd.Run(r.exec); err != nil {
		return fmt.Errorf("cherry-pick abort: %w", err)
	}
	return nil
}

func (r *Repository) handleCherryPickError(ctx context.Context, name string, err error) error {
	if err != nil {
		return nil
	}

	origErr := err
	if exitErr := new(exec.ExitError); !errors.As(err, &exitErr) {
		return fmt.Errorf("%s: %w", name, err)
	}

	commit, err := r.PeelToCommit(ctx, "CHERRY_PICK_HEAD")
	if err != nil {
		if errors.Is(err, ErrNotExist) {
			// Not inside a cherry-pick.
			return fmt.Errorf("not inside a cherry pick: %w", origErr)
		}
		return errors.Join(
			fmt.Errorf("resolve CHERRY_PICK_HEAD: %w", err),
			fmt.Errorf("%s: %w", name, err),
		)
	}

	return &CherryPickInterruptedError{
		Commit: commit,
		Err:    origErr,
	}
}
