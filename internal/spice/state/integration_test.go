package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

func TestStore_Integration_notConfigured(t *testing.T) {
	mem := make(storage.MapBackend)
	_, err := state.InitStore(t.Context(), state.InitStoreRequest{
		DB:    storage.NewDB(mem),
		Trunk: "main",
	})
	require.NoError(t, err)

	store, err := state.OpenStore(t.Context(), storage.NewDB(mem), silogtest.New(t))
	require.NoError(t, err)

	_, err = store.Integration(t.Context())
	assert.ErrorIs(t, err, state.ErrNotExist)
}

func TestStore_SetIntegration(t *testing.T) {
	mem := make(storage.MapBackend)
	_, err := state.InitStore(t.Context(), state.InitStoreRequest{
		DB:    storage.NewDB(mem),
		Trunk: "main",
	})
	require.NoError(t, err)

	store, err := state.OpenStore(t.Context(), storage.NewDB(mem), silogtest.New(t))
	require.NoError(t, err)

	want := &state.IntegrationInfo{
		Name:           "preview",
		UpstreamBranch: "preview",
		Tips: []state.IntegrationTip{
			{Name: "feat-a", Hash: "abc123"},
			{Name: "feat-b", Hash: "def456"},
		},
	}
	require.NoError(t, store.SetIntegration(t.Context(), want))

	t.Run("VersionBumpedToThree", func(t *testing.T) {
		assert.JSONEq(t, `3`, string(mem["version"]))
	})

	t.Run("ReopenAndGet", func(t *testing.T) {
		reopened, err := state.OpenStore(t.Context(), storage.NewDB(mem), silogtest.New(t))
		require.NoError(t, err)

		got, err := reopened.Integration(t.Context())
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})
}

func TestStore_SetIntegration_clear(t *testing.T) {
	mem := make(storage.MapBackend)
	_, err := state.InitStore(t.Context(), state.InitStoreRequest{
		DB:    storage.NewDB(mem),
		Trunk: "main",
	})
	require.NoError(t, err)

	store, err := state.OpenStore(t.Context(), storage.NewDB(mem), silogtest.New(t))
	require.NoError(t, err)

	require.NoError(t, store.SetIntegration(t.Context(), &state.IntegrationInfo{
		Name: "preview",
		Tips: []state.IntegrationTip{{Name: "feat-a"}},
	}))

	// Version was bumped to 3.
	assert.JSONEq(t, `3`, string(mem["version"]))

	// Clear it.
	require.NoError(t, store.SetIntegration(t.Context(), nil))

	// Version should drop back to 1 (no remote, no integration).
	assert.JSONEq(t, `1`, string(mem["version"]))

	_, err = store.Integration(t.Context())
	assert.ErrorIs(t, err, state.ErrNotExist)
}

func TestStore_SetIntegration_validate(t *testing.T) {
	mem := make(storage.MapBackend)
	_, err := state.InitStore(t.Context(), state.InitStoreRequest{
		DB:    storage.NewDB(mem),
		Trunk: "main",
	})
	require.NoError(t, err)

	store, err := state.OpenStore(t.Context(), storage.NewDB(mem), silogtest.New(t))
	require.NoError(t, err)

	tests := []struct {
		name    string
		give    *state.IntegrationInfo
		wantErr string
	}{
		{
			name:    "empty name",
			give:    &state.IntegrationInfo{Name: ""},
			wantErr: "integration branch name is empty",
		},
		{
			name:    "name equals trunk",
			give:    &state.IntegrationInfo{Name: "main"},
			wantErr: "must not equal trunk",
		},
		{
			name: "tip equals trunk",
			give: &state.IntegrationInfo{
				Name: "preview",
				Tips: []state.IntegrationTip{{Name: "main"}},
			},
			wantErr: "tip must not equal trunk",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.SetIntegration(t.Context(), tt.give)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestOpenStore_versionThree(t *testing.T) {
	mem := storage.MapBackend{
		"version": []byte("3"),
		"repo": []byte(`{
			"trunk": "main",
			"integration": {
				"name": "preview",
				"tips": [{"name": "feat-a", "hash": "abc"}]
			}
		}`),
	}

	store, err := state.OpenStore(t.Context(), storage.NewDB(mem), silogtest.New(t))
	require.NoError(t, err)

	got, err := store.Integration(t.Context())
	require.NoError(t, err)
	assert.Equal(t, &state.IntegrationInfo{
		Name: "preview",
		Tips: []state.IntegrationTip{{Name: "feat-a", Hash: "abc"}},
	}, got)
}

func TestStore_SetIntegration_preservesRemote(t *testing.T) {
	mem := make(storage.MapBackend)
	_, err := state.InitStore(t.Context(), state.InitStoreRequest{
		DB:    storage.NewDB(mem),
		Trunk: "main",
		Remote: state.Remote{
			Upstream: "upstream",
			Push:     "origin",
		},
	})
	require.NoError(t, err)

	store, err := state.OpenStore(t.Context(), storage.NewDB(mem), silogtest.New(t))
	require.NoError(t, err)

	require.NoError(t, store.SetIntegration(t.Context(), &state.IntegrationInfo{
		Name: "preview",
	}))

	// Remote should still be intact.
	gotRemote, err := store.Remote()
	require.NoError(t, err)
	assert.Equal(t, state.Remote{Upstream: "upstream", Push: "origin"}, gotRemote)

	// And version is 3 (since integration overrides v2).
	assert.JSONEq(t, `3`, string(mem["version"]))
}
