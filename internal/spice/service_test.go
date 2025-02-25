package spice

import (
	"testing"

	"github.com/charmbracelet/log"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/logutil"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

// NewTestService creates a new Service for testing.
// If forge is nil, it uses the ShamHub forge.
func NewTestService(
	repo GitRepository,
	store Store,
	log *log.Logger,
) *Service {
	return newService(repo, store, log)
}

// NewMemoryStore builds gs state storage
// that stores everything in memory.
// The store is initialized with the trunk "main".
func NewMemoryStore(t *testing.T) *state.Store {
	t.Helper()

	ctx := t.Context()
	db := storage.NewDB(make(storage.MapBackend))
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    db,
		Trunk: "main",
		Log:   logutil.TestLogger(t),
	})
	require.NoError(t, err)

	return store
}
