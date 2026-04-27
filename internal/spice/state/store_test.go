package state_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/statetest"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

func TestStore(t *testing.T) {
	ctx := t.Context()
	db := storage.NewDB(make(storage.MapBackend))

	_, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    db,
		Trunk: "main",
	})
	require.NoError(t, err)

	store, err := state.OpenStore(ctx, db, silogtest.New(t))
	require.NoError(t, err)

	t.Run("empty", func(t *testing.T) {
		_, err := store.LookupBranch(ctx, "foo")
		assert.ErrorIs(t, err, state.ErrNotExist)
	})

	err = statetest.UpdateBranch(ctx, store, &statetest.UpdateRequest{
		Upserts: []state.UpsertRequest{{
			Name:           "foo",
			Base:           "main",
			BaseHash:       "123456",
			ChangeForge:    "shamhub",
			ChangeMetadata: json.RawMessage(`{"number": 42}`),
		}},
	})
	require.NoError(t, err)

	t.Run("get", func(t *testing.T) {
		ctx := t.Context()
		res, err := store.LookupBranch(ctx, "foo")
		require.NoError(t, err)

		assert.Equal(t, "main", res.Base)
		assert.Equal(t, "123456", string(res.BaseHash))
		assert.Equal(t, "shamhub", res.ChangeForge)
		assert.JSONEq(t, `{"number": 42}`, string(res.ChangeMetadata))
	})

	require.NoError(t, statetest.UpdateBranch(ctx, store, &statetest.UpdateRequest{
		Upserts: []state.UpsertRequest{{
			Name:           "bar2",
			Base:           "main",
			BaseHash:       "abcdef",
			ChangeForge:    "shamhub",
			ChangeMetadata: json.RawMessage(`{"id": 42}`),
		}},
	}))

	t.Run("overwrite", func(t *testing.T) {
		ctx := t.Context()
		err := statetest.UpdateBranch(ctx, store, &statetest.UpdateRequest{
			Upserts: []state.UpsertRequest{
				{
					Name:           "foo",
					Base:           "bar2",
					BaseHash:       "54321",
					ChangeForge:    "shamhub",
					ChangeMetadata: json.RawMessage(`{"id": 43}`),
				},
			},
		})
		require.NoError(t, err)

		res, err := store.LookupBranch(ctx, "foo")
		require.NoError(t, err)

		assert.Equal(t, "bar2", res.Base)
		assert.Equal(t, "54321", string(res.BaseHash))
		assert.Equal(t, "shamhub", res.ChangeForge)
		assert.JSONEq(t, `{"id": 43}`, string(res.ChangeMetadata))
	})

	t.Run("name with slash", func(t *testing.T) {
		ctx := t.Context()
		err := statetest.UpdateBranch(ctx, store, &statetest.UpdateRequest{
			Upserts: []state.UpsertRequest{{
				Name:           "bar/baz",
				Base:           "main",
				ChangeForge:    "shamhub",
				ChangeMetadata: json.RawMessage(`{"id": 44}`),
				BaseHash:       "abcdef",
			}},
		})
		require.NoError(t, err)

		res, err := store.LookupBranch(ctx, "bar/baz")
		require.NoError(t, err)
		assert.Equal(t, "main", res.Base)
		assert.Equal(t, "abcdef", string(res.BaseHash))
		assert.Equal(t, "shamhub", res.ChangeForge)
		assert.JSONEq(t, `{"id": 44}`, string(res.ChangeMetadata))
	})

	t.Run("upstream branch", func(t *testing.T) {
		ctx := t.Context()
		upstream := "remoteBranch"
		err := statetest.UpdateBranch(ctx, store, &statetest.UpdateRequest{
			Upserts: []state.UpsertRequest{{
				Name:           "localBranch",
				Base:           "main",
				BaseHash:       "abcdef",
				UpstreamBranch: &upstream,
			}},
		})
		require.NoError(t, err)

		res, err := store.LookupBranch(ctx, "localBranch")
		require.NoError(t, err)

		assert.Equal(t, "main", res.Base)
		assert.Equal(t, "abcdef", string(res.BaseHash))
		assert.Equal(t, "remoteBranch", res.UpstreamBranch)
	})
}

func TestOpenStore_remoteMigration(t *testing.T) {
	tests := []struct {
		name        string
		mem         storage.MapBackend
		want        state.Remote
		wantVersion string
		wantRepo    string
	}{
		{
			name: "ImplicitV1",
			mem: storage.MapBackend{
				"repo": []byte(`{"trunk":"main","remote":"origin"}`),
			},
			want: state.Remote{
				Upstream: "origin",
				Push:     "origin",
			},
			wantRepo: `{"trunk":"main","remote":"origin"}`,
		},
		{
			name: "ExplicitV1",
			mem: storage.MapBackend{
				"version": []byte("1"),
				"repo":    []byte(`{"trunk":"main","remote":"origin"}`),
			},
			want: state.Remote{
				Upstream: "origin",
				Push:     "origin",
			},
			wantVersion: `1`,
			wantRepo:    `{"trunk":"main","remote":"origin"}`,
		},
		{
			name: "ExplicitV2",
			mem: storage.MapBackend{
				"version": []byte("2"),
				"repo": []byte(
					`{"trunk":"main","remotes":{"upstream":"upstream","push":"origin"}}`,
				),
			},
			want: state.Remote{
				Upstream: "upstream",
				Push:     "origin",
			},
			wantVersion: `2`,
			wantRepo: `{
				"trunk": "main",
				"remotes": {
					"upstream": "upstream",
					"push": "origin"
				}
			}`,
		},
		{
			name: "PreviousV2RemoteObject",
			mem: storage.MapBackend{
				"version": []byte("2"),
				"repo": []byte(
					`{"trunk":"main","remote":{"upstream":"upstream","push":"origin"}}`,
				),
			},
			want: state.Remote{
				Upstream: "upstream",
				Push:     "origin",
			},
			wantVersion: `2`,
			wantRepo: `{
				"trunk": "main",
				"remote": {
					"upstream": "upstream",
					"push": "origin"
				}
			}`,
		},
		{
			name: "OmittedRemote",
			mem: storage.MapBackend{
				"repo": []byte(`{"trunk":"main"}`),
			},
			wantRepo: `{"trunk":"main"}`,
		},
		{
			name: "EmptyLegacyRemote",
			mem: storage.MapBackend{
				"repo": []byte(`{"trunk":"main","remote":""}`),
			},
			wantRepo: `{"trunk":"main","remote":""}`,
		},
		{
			name: "EmptyRemoteObject",
			mem: storage.MapBackend{
				"version": []byte("2"),
				"repo":    []byte(`{"trunk":"main","remote":{}}`),
			},
			wantVersion: `2`,
			wantRepo:    `{"trunk":"main","remote":{}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := state.OpenStore(
				t.Context(),
				storage.NewDB(tt.mem),
				silogtest.New(t),
			)
			require.NoError(t, err)

			got, err := store.Remote()
			if tt.want == (state.Remote{}) {
				require.ErrorIs(t, err, state.ErrNotExist)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}

			if tt.wantVersion == "" {
				assert.Empty(t, tt.mem["version"])
			} else {
				assert.JSONEq(t, tt.wantVersion, string(tt.mem["version"]))
			}
			assert.JSONEq(t, tt.wantRepo, string(tt.mem["repo"]))
		})
	}
}

func TestOpenStore_remoteMigrationMalformed(t *testing.T) {
	mem := storage.MapBackend{
		"repo": []byte(`{"trunk":"main","remote":1}`),
	}

	_, err := state.OpenStore(t.Context(), storage.NewDB(mem), nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "get repo state:")
}

func TestInitStore_writesVersionOneForSameRemote(t *testing.T) {
	mem := make(storage.MapBackend)
	_, err := state.InitStore(t.Context(), state.InitStoreRequest{
		DB:    storage.NewDB(mem),
		Trunk: "main",
		Remote: state.Remote{
			Upstream: "origin",
			Push:     "origin",
		},
	})
	require.NoError(t, err)

	assert.JSONEq(t, `1`, string(mem["version"]))
	assert.JSONEq(t, `{
		"trunk": "main",
		"remote": "origin"
	}`, string(mem["repo"]))
}

func TestInitStore_writesVersionTwoRemotesObjectForForkMode(t *testing.T) {
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

	assert.JSONEq(t, `2`, string(mem["version"]))
	assert.JSONEq(t, `{
		"trunk": "main",
		"remote": "upstream",
		"remotes": {
			"upstream": "upstream",
			"push": "origin"
		}
	}`, string(mem["repo"]))
}

func TestStore_SetRemote(t *testing.T) {
	mem := storage.MapBackend{
		"repo": []byte(`{"trunk":"main","remote":"origin"}`),
	}
	store, err := state.OpenStore(t.Context(), storage.NewDB(mem), nil)
	require.NoError(t, err)

	err = store.SetRemote(t.Context(), state.Remote{
		Upstream: "upstream",
		Push:     "origin",
	})
	require.NoError(t, err)

	assert.JSONEq(t, `2`, string(mem["version"]))
	assert.JSONEq(t, `{
		"trunk": "main",
		"remote": "upstream",
		"remotes": {
			"upstream": "upstream",
			"push": "origin"
		}
	}`, string(mem["repo"]))

	got, err := store.Remote()
	require.NoError(t, err)
	assert.Equal(t, state.Remote{
		Upstream: "upstream",
		Push:     "origin",
	}, got)
}

func TestStore_SetRemote_downgradesToVersionOneForSameRemote(t *testing.T) {
	mem := storage.MapBackend{
		"version": []byte("2"),
		"repo": []byte(
			`{"trunk":"main","remote":"upstream","remotes":{"upstream":"upstream","push":"origin"}}`,
		),
	}
	store, err := state.OpenStore(t.Context(), storage.NewDB(mem), nil)
	require.NoError(t, err)

	err = store.SetRemote(t.Context(), state.Remote{
		Upstream: "origin",
		Push:     "origin",
	})
	require.NoError(t, err)

	assert.JSONEq(t, `1`, string(mem["version"]))
	assert.JSONEq(t, `{
		"trunk": "main",
		"remote": "origin"
	}`, string(mem["repo"]))

	got, err := store.Remote()
	require.NoError(t, err)
	assert.Equal(t, state.Remote{
		Upstream: "origin",
		Push:     "origin",
	}, got)
}

func TestOpenStore_errors(t *testing.T) {
	t.Run("VersionMismatch", func(t *testing.T) {
		mem := storage.MapBackend{
			"version": []byte("500"),
		}

		_, err := state.OpenStore(t.Context(), storage.NewDB(mem), nil)
		require.Error(t, err)
		assert.ErrorContains(t, err, "check store layout:")
		assert.ErrorAs(t, err, new(*state.VersionMismatchError))
	})

	t.Run("NotInitialized", func(t *testing.T) {
		mem := make(storage.MapBackend)
		_, err := state.OpenStore(t.Context(), storage.NewDB(mem), nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, state.ErrUninitialized)
	})

	t.Run("CorruptRepo/Unparseable", func(t *testing.T) {
		mem := storage.MapBackend{
			"repo": []byte(`{`),
		}

		_, err := state.OpenStore(t.Context(), storage.NewDB(mem), nil)
		require.Error(t, err)
		assert.ErrorContains(t, err, "get repo state:")
	})

	t.Run("CorruptRepo/Incomplete", func(t *testing.T) {
		mem := storage.MapBackend{
			"repo": []byte(`{}`),
		}

		_, err := state.OpenStore(t.Context(), storage.NewDB(mem), nil)
		require.Error(t, err)
		assert.ErrorContains(t, err, "corrupt state:")
	})
}
