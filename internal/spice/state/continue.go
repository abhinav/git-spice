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

// AppendContinuation records a command that should run
// when an interrupted rebase operation is resumed.
// If there are existing continuations, this will append to the list.
func (s *Store) AppendContinuation(ctx context.Context, req SetContinuationRequest) error {
	must.NotBeBlankf(req.Branch, "a branch name is required")
	must.NotBeEmptyf(req.Command, "arguments for git-spice are required")
	if req.Message == "" {
		req.Message = "set rebase continuation"
	}

	state, err := s.getRebaseContinueState(ctx)
	if err != nil {
		return fmt.Errorf("get rebase continue state: %w", err)
	}

	state.Continuations = append(state.Continuations, rebaseContinuation{
		Branch:  req.Branch,
		Command: req.Command,
	})

	if err := s.setRebaseContinueState(ctx, *state, req.Message); err != nil {
		return fmt.Errorf("set rebase continue state: %w", err)
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

	state, err := s.getRebaseContinueState(ctx)
	if err != nil {
		return nil, fmt.Errorf("get rebase continue state: %w", err)
	}

	if len(state.Continuations) == 0 {
		return nil, nil
	}

	cont := state.Continuations[0]
	state.Continuations = state.Continuations[1:]

	if err := s.setRebaseContinueState(ctx, *state, msg); err != nil {
		return nil, fmt.Errorf("set rebase continue state: %w", err)
	}

	return &TakeContinuationResult{
		Command: cont.Command,
		Branch:  cont.Branch,
	}, nil
}

func (s *Store) getRebaseContinueState(ctx context.Context) (*rebaseContinueState, error) {
	var state rebaseContinueState
	if err := s.b.Get(ctx, _rebaseContinueJSON, &state); err != nil {
		if errors.Is(err, ErrNotExist) {
			return &rebaseContinueState{}, nil
		}
		return nil, fmt.Errorf("get rebase continue state: %w", err)
	}
	return &state, nil
}

func (s *Store) setRebaseContinueState(ctx context.Context, state rebaseContinueState, msg string) error {
	if msg == "" {
		msg = "set rebase continue state"
	}
	return s.b.Update(ctx, updateRequest{
		Sets: []setRequest{
			{Key: _rebaseContinueJSON, Val: state},
		},
		Msg: msg,
	})
}
