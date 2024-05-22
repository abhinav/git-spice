package spice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/spice/state"
)

// ErrRebaseInterrupted indicates that a rebase operation was interrupted.
var ErrRebaseInterrupted = errors.New("rebase interrupted")

// RebaseRescueRequest is a request to rescue a rebase operation.
type RebaseRescueRequest struct {
	// Err is the error that caused the rebase operation to be interrupted.
	Err error

	// Command is the command that should be run
	// after the rebase operation has been rescued.
	//
	// If this is unset, a continuation will NOT be recorded.
	Command []string

	// Branch is the branch on which the command should be run.
	//
	// If this is unset, the continuation will run on the interrupted
	// branch.
	Branch string

	// Message is the message that should be recorded
	// for debugging this continuation.
	Message string // optional
}

// RebaseRescue attempts to recover a git-spice operation that was interrupted
// by a rebase conflict or other interruption.
// If it determines that the rebase can be recovered from and continued in the
// future, it records the continuation command in the data store for later
// resumption.
//
// This returns [ErrRebaseInterrupted] if the rebase was recovered from
// so that the program can exit and the oepration can resume later.
func (s *Service) RebaseRescue(ctx context.Context, req RebaseRescueRequest) error {
	if req.Err == nil {
		return nil
	}

	var rebaseErr *git.RebaseInterruptError
	if !errors.As(req.Err, &rebaseErr) {
		return req.Err
	}

	// TODO: This will also log git's standard advice for resolving conflicts.
	// We could suppress that by setting advice.mergeConflict=false
	// during the rebase operation.
	s.log.Warn("rebase interrupted", "error", rebaseErr)

	switch rebaseErr.Kind {
	case git.RebaseInterruptConflict:
		var msg strings.Builder
		fmt.Fprintf(&msg, "There was a conflict while rebasing.\n")
		fmt.Fprintf(&msg, "Resolve the conflict and run:\n")
		fmt.Fprintf(&msg, "  gs rebase continue\n")
		fmt.Fprintf(&msg, "Or abort the operation with:\n")
		fmt.Fprintf(&msg, "  gs rebase abort\n")
		s.log.Error(msg.String())
	case git.RebaseInterruptDeliberate:
		var msg strings.Builder
		fmt.Fprintf(&msg, "The rebase operation was interrupted with an 'edit' or 'break' command.\n")
		fmt.Fprintf(&msg, "When you're ready to continue, run:\n")
		fmt.Fprintf(&msg, "  gs rebase continue\n")
		fmt.Fprintf(&msg, "Or abort the operation with:\n")
		fmt.Fprintf(&msg, "  gs rebase abort\n")
		s.log.Info(msg.String())
	default:
		must.Failf("unexpected rebase interrupt kind: %v", rebaseErr.Kind)
	}

	// No continuation to record.
	if len(req.Command) == 0 {
		return ErrRebaseInterrupted
	}

	branch := req.Branch
	if branch == "" {
		branch = rebaseErr.State.Branch
	}

	msg := req.Message
	if msg == "" {
		msg = fmt.Sprintf("interrupted: branch %s", req.Branch)
	}

	if err := s.store.SetContinuation(ctx, state.SetContinuationRequest{
		Command: req.Command,
		Branch:  branch,
		Message: msg,
	}); err != nil {
		return fmt.Errorf("edit state: %w", err)
	}

	return ErrRebaseInterrupted
}
