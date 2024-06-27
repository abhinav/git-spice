package state

import (
	"context"
	"errors"
	"fmt"
)

const _rebaseContinueJSON = "rebase-continue"

type rebaseContinueState struct {
	Continuations []rebaseContinuation `json:"continuations"`
}

type rebaseContinuation struct {
	// Command is the gs command that will be run.
	Command []string `json:"command"`

	// Branch on which the command must be run.
	Branch string `json:"branch"`
}

// Continuation includes the information needed to resume a
// rebase operation that was interrupted.
type Continuation struct {
	// Command specifies the arguments for the gs operation
	// that was interrupted.
	Command []string

	// Branch is the branch that the command should be run on.
	Branch string
}

// AppendContinuations records one or more commands to run
// when an interrupted rebase operation is resumed.
// If there are existing continuations, this will append to the list.
func (s *Store) AppendContinuations(ctx context.Context, msg string, conts ...Continuation) error {
	if msg == "" {
		msg = "set rebase continuation"
	}

	state, err := s.getRebaseContinueState(ctx)
	if err != nil {
		return fmt.Errorf("get rebase continue state: %w", err)
	}

	for _, cont := range conts {
		state.Continuations = append(state.Continuations, rebaseContinuation(cont))
	}

	if err := s.setRebaseContinueState(ctx, state, msg); err != nil {
		return fmt.Errorf("set rebase continue state: %w", err)
	}

	return nil
}

// TakeContinuations removes all recorded rebase continuations from the store
// and returns them.
//
// If there are no continuations, it returns an empty slice.
func (s *Store) TakeContinuations(ctx context.Context, msg string) ([]Continuation, error) {
	if msg == "" {
		msg = "take all rebase continuations"
	}

	state, err := s.getRebaseContinueState(ctx)
	if err != nil {
		return nil, fmt.Errorf("get rebase continue state: %w", err)
	}

	if len(state.Continuations) == 0 {
		return nil, nil
	}

	conts := make([]Continuation, len(state.Continuations))
	for i, cont := range state.Continuations {
		conts[i] = Continuation(cont)
	}

	state.Continuations = nil
	if err := s.setRebaseContinueState(ctx, state, msg); err != nil {
		return nil, fmt.Errorf("set rebase continue state: %w", err)
	}

	return conts, nil
}

func (s *Store) getRebaseContinueState(ctx context.Context) (*rebaseContinueState, error) {
	var state rebaseContinueState
	if err := s.db.Get(ctx, _rebaseContinueJSON, &state); err != nil {
		if errors.Is(err, ErrNotExist) {
			return &rebaseContinueState{}, nil
		}
		return nil, fmt.Errorf("get rebase continue state: %w", err)
	}
	return &state, nil
}

func (s *Store) setRebaseContinueState(ctx context.Context, state *rebaseContinueState, msg string) error {
	if msg == "" {
		msg = "set rebase continue state"
	}
	return s.db.Set(ctx, _rebaseContinueJSON, state, msg)
}
