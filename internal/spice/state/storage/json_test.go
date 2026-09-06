package storage

import (
	"context"
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/jsonmut"
)

func TestMutateJSON_replaysConflict(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	backend := &conflictOnceBackend{
		Backend: SyncBackend(make(MapBackend)),
		BeforeConflict: func(ctx context.Context, backend Backend) error {
			return backend.Update(ctx, UpdateRequest{
				Sets: []SetRequest{{
					Key: "comments/feature",
					Value: jsontext.Value(`{
						"lastID": 1,
						"drafts": {"1": {"body": "concurrent"}}
					}`),
				}},
				Message: "concurrent insertion",
			})
		},
	}

	id, err := MutateJSON(
		ctx,
		backend,
		JSONMutationRequest{
			Key:       "comments/feature",
			IfMissing: jsontext.Value(`{}`),
			Message:   "insert draft",
		},
		jsonmut.InsertAutoIncrement(
			"/lastID",
			"/drafts",
			jsontext.Value(`{"body":"requested"}`),
		),
	)
	require.NoError(t, err)
	assert.Equal(t, int64(2), id)

	var document jsontext.Value
	require.NoError(t, backend.Get(ctx, "comments/feature", &document))
	assert.JSONEq(t, `{
		"lastID": 2,
		"drafts": {
			"1": {"body": "concurrent"},
			"2": {"body": "requested"}
		}
	}`, document.String())
}

func TestMutateJSON_rechecksRequirementsAfterConflict(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	underlying := SyncBackend(make(MapBackend))
	require.NoError(t, underlying.Update(ctx, UpdateRequest{
		Sets:    []SetRequest{{Key: "branches/feature", Value: struct{}{}}},
		Message: "track branch",
	}))
	backend := &conflictOnceBackend{
		Backend: underlying,
		BeforeConflict: func(ctx context.Context, backend Backend) error {
			return backend.Update(ctx, UpdateRequest{
				Deletes: []string{"branches/feature"},
				Message: "untrack branch",
			})
		},
	}

	_, err := MutateJSON(
		ctx,
		backend,
		JSONMutationRequest{
			Key:       "comments/feature",
			Requires:  []string{"branches/feature"},
			IfMissing: jsontext.Value(`{}`),
			Message:   "insert draft",
		},
		jsonmut.InsertAutoIncrement(
			"/lastID",
			"/drafts",
			jsontext.Value(`{"body":"requested"}`),
		),
	)
	assert.ErrorIs(t, err, ErrNotExist)

	var document jsontext.Value
	assert.ErrorIs(
		t,
		backend.Get(ctx, "comments/feature", &document),
		ErrNotExist,
	)
}

func TestMutateJSON_missingWithoutDefault(t *testing.T) {
	t.Parallel()

	backend := SyncBackend(make(MapBackend))
	_, err := MutateJSON(
		t.Context(),
		backend,
		JSONMutationRequest{
			Key:     "missing",
			Message: "mutate missing value",
		},
		jsonmut.Set("/value", jsontext.Value("1")),
	)
	assert.ErrorIs(t, err, ErrNotExist)

	var document jsontext.Value
	assert.ErrorIs(t, backend.Get(t.Context(), "missing", &document), ErrNotExist)
}

type conflictOnceBackend struct {
	Backend
	BeforeConflict func(context.Context, Backend) error
	conflicted     bool
}

func (b *conflictOnceBackend) CompareAndSwap(
	ctx context.Context,
	snapshot Snapshot,
	req UpdateRequest,
) error {
	if !b.conflicted {
		b.conflicted = true
		if err := b.BeforeConflict(ctx, b.Backend); err != nil {
			return err
		}
		return ErrConflict
	}
	return b.Backend.CompareAndSwap(ctx, snapshot, req)
}
