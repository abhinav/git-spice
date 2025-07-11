package spice

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

// NewTestService creates a new Service for testing.
// If forge is nil, it uses the ShamHub forge.
func NewTestService(
	repo GitRepository,
	wt GitWorktree,
	store Store,
	forgeReg *forge.Registry,
	log *silog.Logger,
) *Service {
	return newService(repo, wt, store, forgeReg, log)
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
		Log:   silogtest.New(t),
	})
	require.NoError(t, err)

	return store
}
