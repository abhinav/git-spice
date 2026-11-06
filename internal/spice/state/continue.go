package state

import (
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/jsonmut"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

const _rebaseContinueJSON = "rebase-continue"

const _continuationsPointer = jsontext.Pointer("/continuations")

const _emptyRebaseContinueJSON = `{"continuations":null}`

// Continuation includes the information needed to resume a
// rebase operation that was interrupted.
type Continuation struct {
	// Command specifies the arguments for the git-spice operation
	// that was interrupted.
	Command []string

	// Branch is the branch that the command should be run on.
	Branch string
}

type rebaseContinuation struct {
	// Command is the git-spice command that will be run.
	Command []string `json:"command"`

	// Branch on which the command must be run.
	Branch string `json:"branch"`
}

// AppendContinuations records one or more commands to run
// when an interrupted rebase operation is resumed.
// If there are existing continuations, this will append to the list.
// Concurrent appends and takes are applied atomically.
func (s *Store) AppendContinuations(ctx context.Context, msg string, conts ...Continuation) error {
	if msg == "" {
		msg = "set rebase continuation"
	}

	additions := make([]rebaseContinuation, len(conts))
	for i, cont := range conts {
		additions[i] = rebaseContinuation{
			Command: slices.Clone(cont.Command),
			Branch:  cont.Branch,
		}
	}

	// The continuation receives freshly decoded state on every CAS attempt.
	// Keep captured additions immutable so a conflict can replay the program.
	program := jsonmut.Decode[[]rebaseContinuation](_continuationsPointer).
		Then(func(current []rebaseContinuation) jsonmut.Statement {
			updated, err := json.Marshal(slices.Concat(current, additions))
			if err != nil {
				return jsonmut.Fail[struct{}](fmt.Errorf("marshal continuations: %w", err))
			}
			return jsonmut.Set(_continuationsPointer, updated)
		})
	if err := storage.UpdateJSON(ctx, s.db, storage.JSONMutationRequest{
		Key:       _rebaseContinueJSON,
		IfMissing: jsontext.Value(_emptyRebaseContinueJSON),
		Message:   msg,
	}, program); err != nil {
		return fmt.Errorf("append rebase continuations: %w", err)
	}

	return nil
}

// TakeContinuations removes all recorded rebase continuations from the store
// and returns them.
//
// If there are no continuations, it returns an empty slice.
// Concurrent appends and takes are applied atomically.
func (s *Store) TakeContinuations(ctx context.Context, msg string) ([]Continuation, error) {
	if msg == "" {
		msg = "take all rebase continuations"
	}

	// Clearing the value and returning its previous contents must happen in the
	// same CAS attempt. A replay therefore observes the winning revision instead
	// of returning continuations already taken by another caller.
	program := jsonmut.Decode[[]rebaseContinuation](_continuationsPointer).
		Then(func(current []rebaseContinuation) jsonmut.Program[[]rebaseContinuation] {
			return jsonmut.Set(
				_continuationsPointer,
				jsontext.Value("null"),
			).Returning(current)
		})
	stored, err := storage.MutateJSON(ctx, s.db, storage.JSONMutationRequest{
		Key:     _rebaseContinueJSON,
		Message: msg,
	}, program)
	if err != nil {
		if errors.Is(err, storage.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("take rebase continuations: %w", err)
	}

	if len(stored) == 0 {
		return nil, nil
	}

	conts := make([]Continuation, len(stored))
	for i, cont := range stored {
		conts[i] = Continuation(cont)
	}
	return conts, nil
}
