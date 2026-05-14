package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

func TestStore_PendingIntegrationRebuild_notExist(t *testing.T) {
	mem := make(storage.MapBackend)
	_, err := state.InitStore(t.Context(), state.InitStoreRequest{
		DB:    storage.NewDB(mem),
		Trunk: "main",
	})
	require.NoError(t, err)

	store, err := state.OpenStore(t.Context(), storage.NewDB(mem), silogtest.New(t))
	require.NoError(t, err)

	_, err = store.PendingIntegrationRebuild(t.Context())
	assert.ErrorIs(t, err, state.ErrNotExist)
}

func TestStore_SetPendingIntegrationRebuild_roundTrip(t *testing.T) {
	mem := make(storage.MapBackend)
	_, err := state.InitStore(t.Context(), state.InitStoreRequest{
		DB:    storage.NewDB(mem),
		Trunk: "main",
	})
	require.NoError(t, err)

	store, err := state.OpenStore(t.Context(), storage.NewDB(mem), silogtest.New(t))
	require.NoError(t, err)

	want := &state.IntegrationRebuild{
		Integration: "preview",
		Tips: []state.IntegrationTip{
			{Name: "feat-a", Hash: "hash-a"},
			{Name: "feat-b", Hash: "hash-b"},
			{Name: "feat-c", Hash: "hash-c"},
		},
		NextTipIndex: 2,
	}
	require.NoError(t, store.SetPendingIntegrationRebuild(t.Context(), want))

	got, err := store.PendingIntegrationRebuild(t.Context())
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestStore_ClearPendingIntegrationRebuild(t *testing.T) {
	mem := make(storage.MapBackend)
	_, err := state.InitStore(t.Context(), state.InitStoreRequest{
		DB:    storage.NewDB(mem),
		Trunk: "main",
	})
	require.NoError(t, err)

	store, err := state.OpenStore(t.Context(), storage.NewDB(mem), silogtest.New(t))
	require.NoError(t, err)

	require.NoError(t, store.SetPendingIntegrationRebuild(t.Context(), &state.IntegrationRebuild{
		Integration: "preview",
	}))

	require.NoError(t, store.ClearPendingIntegrationRebuild(t.Context()))

	_, err = store.PendingIntegrationRebuild(t.Context())
	assert.ErrorIs(t, err, state.ErrNotExist)

	// Idempotent
	require.NoError(t, store.ClearPendingIntegrationRebuild(t.Context()))
}
