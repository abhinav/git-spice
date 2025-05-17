// Package statetest provides utilities for testing code
// that makes use of the state package.
package statetest

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/spice/state"
)

// UpdateRequest is a request to add, update, or delete information about branches.
type UpdateRequest struct {
	// Upserts are requests to add or update information about branches.
	Upserts []state.UpsertRequest

	// Deletes are requests to delete information about branches.
	Deletes []string

	// Message is a message specifying the reason for the update.
	// This will be persisted in the Git commit message.
	Message string
}

// UpdateBranch applies an UpdateRequest to the given Store in a transaction.
func UpdateBranch(ctx context.Context, s *state.Store, req *UpdateRequest) error {
	tx := s.BeginBranchTx()
	for idx, upsert := range req.Upserts {
		if err := tx.Upsert(ctx, upsert); err != nil {
			return fmt.Errorf("upsert [%d] %q: %w", idx, upsert.Name, err)
		}
	}

	for idx, name := range req.Deletes {
		if err := tx.Delete(ctx, name); err != nil {
			return fmt.Errorf("delete [%d] %q: %w", idx, name, err)
		}
	}

	if err := tx.Commit(ctx, req.Message); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}
