package state

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/must"
)

// SetContinuationRequest is a request to set the operation
// that should run after the current rebase finishes successfully.
type SetContinuationRequest struct {
	// Branch is the branch on which the operation should run.
	Branch string // required

	// Command specifies the gs command that will be run.
	Command []string // required

	// Message is a message for the gs state log.
	Message string
}

// SetContinuation records a command that should run
// when an interrupted rebase operation is resumed.
func (s *Store) SetContinuation(ctx context.Context, req SetContinuationRequest) error {
	must.NotBeBlankf(req.Branch, "a branch name is required")
	must.NotBeEmptyf(req.Command, "arguments for git-spice are required")
	if req.Message == "" {
		req.Message = "set rebase continuation"
	}

	// Sanity check:
	// Must not have an existing continuation.
	var cont rebaseContinuation
	if err := s.b.Get(ctx, _rebaseContinueJSON, &cont); err == nil {
		s.log.Errorf("Found an existing rebase continuation for %v: %q", cont.Branch, cont.Command)
		return errors.New("an unfinished rebase continuation already exists")
		// TODO: If we encounter this in practice from a normal workflow,
		// we'll probably want a queue or stack for continuations.
	}

	cont = rebaseContinuation{
		Branch:  req.Branch,
		Command: req.Command,
	}
	if err := s.b.Update(ctx, updateRequest{
		Sets: []setRequest{
			{Key: _rebaseContinueJSON, Val: cont},
		},
		Msg: req.Message,
	}); err != nil {
		return fmt.Errorf("set rebase continuation: %w", err)
	}

	return nil
}

// TakeContinuationResult includes the information needed to resume a
// rebase operation that was interrupted.
type TakeContinuationResult struct {
	// Command specifies the arguments for the gs operation
	// that was interrupted.
	Command []string

	// Branch is the branch that the command should be run on.
	Branch string
}

// TakeContinuation removes a recorded rebase continuation from the store
// and returns it.
//
// If there is no continuation, it returns nil.
func (s *Store) TakeContinuation(ctx context.Context, msg string) (*TakeContinuationResult, error) {
	if msg == "" {
		msg = "take rebase continuation"
	}

	var cont rebaseContinuation
	if err := s.b.Get(ctx, _rebaseContinueJSON, &cont); err != nil {
		if errors.Is(err, ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("get rebase continuation: %w", err)
	}

	if err := s.b.Update(ctx, updateRequest{
		Dels: []string{_rebaseContinueJSON},
		Msg:  msg,
	}); err != nil {
		return nil, fmt.Errorf("delete rebase continuation: %w", err)
	}

	return &TakeContinuationResult{
		Command: cont.Command,
		Branch:  cont.Branch,
	}, nil
}
