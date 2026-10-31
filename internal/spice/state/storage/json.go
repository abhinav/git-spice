package storage

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/jsonmut"
)

// JSONMutationRequest identifies the stored JSON value to mutate.
type JSONMutationRequest struct {
	Key string // required

	// Requires lists keys that must exist in the committed revision.
	Requires []string

	// IfMissing supplies the document when Key does not exist.
	// If empty, a missing Key returns [ErrNotExist].
	IfMissing jsontext.Value

	// Message is attached to the committed storage update.
	Message string // required
}

// MutateJSON applies program to one JSON value and retries storage conflicts.
// The returned result belongs to the storage revision that committed.
func MutateJSON[T any](
	ctx context.Context,
	backend Backend,
	req JSONMutationRequest,
	program jsonmut.Program[T],
) (T, error) {
	var zero T
	for {
		if err := ctx.Err(); err != nil {
			return zero, err
		}

		snapshot, err := backend.Snapshot(ctx)
		if err != nil {
			return zero, fmt.Errorf("snapshot store: %w", err)
		}
		var document jsontext.Value
		values := make([]jsontext.Value, len(req.Requires))
		reads := make([]GetRequest, len(req.Requires)+1)
		for i, key := range req.Requires {
			reads[i] = GetRequest{Key: key, Value: &values[i]}
		}
		reads[len(req.Requires)] = GetRequest{Key: req.Key, Value: &document}
		if err := snapshot.GetMany(ctx, reads); err != nil {
			missing, ok := errors.AsType[*MissingKeysError](err)
			if !ok {
				return zero, fmt.Errorf("read mutation state for %q: %w", req.Key, err)
			}
			// A missing target may use its default, but every requirement must
			// exist in this same revision, including after a CAS retry.
			for _, index := range missing.Indexes {
				if index < len(req.Requires) {
					return zero, fmt.Errorf("require %q: %w", req.Requires[index], ErrNotExist)
				}
			}
			if len(req.IfMissing) == 0 {
				return zero, ErrNotExist
			}
			document = req.IfMissing.Clone()
		}

		updated, result, err := jsonmut.Apply(document, program)
		if err != nil {
			return zero, fmt.Errorf("mutate %q: %w", req.Key, err)
		}
		err = backend.CompareAndSwap(ctx, snapshot, UpdateRequest{
			Sets:    []SetRequest{{Key: req.Key, Value: updated}},
			Message: req.Message,
		})
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, ErrConflict) {
			return zero, fmt.Errorf("commit %q: %w", req.Key, err)
		}
	}
}

// UpdateJSON applies statement to one JSON value and retries storage conflicts.
func UpdateJSON(
	ctx context.Context,
	backend Backend,
	req JSONMutationRequest,
	statement jsonmut.Statement,
) error {
	_, err := MutateJSON(ctx, backend, req, statement)
	return err
}
