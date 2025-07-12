// Package statetest provides utilities for testing code
// that makes use of the state package.
package statetest

import (
	"cmp"
	"context"
	"fmt"
	"testing"

	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

// NewMemoryStore creates a new, fully initialized in-memory state store.
//
// Returns a function that will reset the store to its initial state
// so that it can be reused across tests.
func NewMemoryStore(t testing.TB, trunk, remote string, log *silog.Logger) *state.Store {
	db := storage.NewDB(storage.SyncBackend(make(storage.MapBackend)))

	if log == nil {
		log = silogtest.New(t)
	}

	store, err := state.InitStore(t.Context(), state.InitStoreRequest{
		DB:     db,
		Trunk:  cmp.Or(trunk, "main"),
		Remote: remote,
		Log:    log,
	})
	if err != nil {
		t.Fatalf("failed to initialize store: %v", err)
	}
	return store
}

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
