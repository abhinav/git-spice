package state_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

func TestStoreAppendContinuations_concurrent(t *testing.T) {
	backend := newReadBarrierBackend(
		storage.SyncBackend(make(storage.MapBackend)),
	)
	store, err := state.InitStore(t.Context(), state.InitStoreRequest{
		DB:    storage.NewDB(backend),
		Trunk: "main",
	})
	require.NoError(t, err)

	first := state.Continuation{
		Command: []string{"branch", "restack"},
		Branch:  "first",
	}
	second := state.Continuation{
		Command: []string{"branch", "restack"},
		Branch:  "second",
	}

	backend.barrierReads("rebase-continue", 2)
	errs := make(chan error, 2)
	go func() {
		errs <- store.AppendContinuations(t.Context(), "append first", first)
	}()
	go func() {
		errs <- store.AppendContinuations(t.Context(), "append second", second)
	}()
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)

	got, err := store.TakeContinuations(t.Context(), "inspect continuations")
	require.NoError(t, err)
	assert.ElementsMatch(t, []state.Continuation{first, second}, got)
}

func TestStoreTakeContinuations_concurrent(t *testing.T) {
	backend := newReadBarrierBackend(
		storage.SyncBackend(make(storage.MapBackend)),
	)
	store, err := state.InitStore(t.Context(), state.InitStoreRequest{
		DB:    storage.NewDB(backend),
		Trunk: "main",
	})
	require.NoError(t, err)

	want := state.Continuation{
		Command: []string{"branch", "restack"},
		Branch:  "feature",
	}
	require.NoError(t, store.AppendContinuations(
		t.Context(),
		"seed continuation",
		want,
	))

	backend.barrierReads("rebase-continue", 2)
	takes := make(chan []state.Continuation, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			got, err := store.TakeContinuations(t.Context(), "take continuations")
			takes <- got
			errs <- err
		}()
	}

	var got []state.Continuation
	for range 2 {
		got = append(got, <-takes...)
		require.NoError(t, <-errs)
	}
	assert.Equal(t, []state.Continuation{want}, got)
}

func TestStoreTakeContinuations_missing(t *testing.T) {
	backend := storage.SyncBackend(make(storage.MapBackend))
	store, err := state.InitStore(t.Context(), state.InitStoreRequest{
		DB:    storage.NewDB(backend),
		Trunk: "main",
	})
	require.NoError(t, err)

	got, err := store.TakeContinuations(t.Context(), "take continuations")
	require.NoError(t, err)
	assert.Empty(t, got)

	var document any
	assert.ErrorIs(
		t,
		backend.Get(t.Context(), "rebase-continue", &document),
		storage.ErrNotExist,
	)
}

func TestStoreContinuations_concurrentAppendAndTake(t *testing.T) {
	backend := newReadBarrierBackend(
		storage.SyncBackend(make(storage.MapBackend)),
	)
	store, err := state.InitStore(t.Context(), state.InitStoreRequest{
		DB:    storage.NewDB(backend),
		Trunk: "main",
	})
	require.NoError(t, err)

	first := state.Continuation{
		Command: []string{"branch", "restack"},
		Branch:  "first",
	}
	second := state.Continuation{
		Command: []string{"branch", "restack"},
		Branch:  "second",
	}
	require.NoError(t, store.AppendContinuations(
		t.Context(),
		"seed continuation",
		first,
	))

	backend.barrierReads("rebase-continue", 2)
	appendErr := make(chan error, 1)
	go func() {
		appendErr <- store.AppendContinuations(
			t.Context(),
			"append continuation",
			second,
		)
	}()
	takenResult := make(chan []state.Continuation, 1)
	takeErr := make(chan error, 1)
	go func() {
		got, err := store.TakeContinuations(t.Context(), "take continuations")
		takenResult <- got
		takeErr <- err
	}()

	require.NoError(t, <-appendErr)
	require.NoError(t, <-takeErr)
	taken := <-takenResult
	remaining, err := store.TakeContinuations(
		t.Context(),
		"inspect remaining continuations",
	)
	require.NoError(t, err)
	assert.ElementsMatch(
		t,
		[]state.Continuation{first, second},
		append(taken, remaining...),
	)
}

// readBarrierBackend holds a configured number of completed reads until every
// reader has observed the backend. It wraps both direct reads and snapshot reads
// so the tests exercise the public state operations before and after their CAS
// migration.
type readBarrierBackend struct {
	storage.Backend

	mu        sync.Mutex
	key       string
	remaining int
	ready     chan struct{}
}

func newReadBarrierBackend(backend storage.Backend) *readBarrierBackend {
	return &readBarrierBackend{Backend: backend}
}

func (b *readBarrierBackend) barrierReads(key string, count int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.key = key
	b.remaining = count
	b.ready = make(chan struct{})
}

func (b *readBarrierBackend) Get(
	ctx context.Context,
	key string,
	dst any,
) error {
	err := b.Backend.Get(ctx, key, dst)
	b.afterRead(key)
	return err
}

func (b *readBarrierBackend) Snapshot(ctx context.Context) (storage.Snapshot, error) {
	snapshot, err := b.Backend.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	return &readBarrierSnapshot{
		Snapshot:  snapshot,
		afterRead: b.afterRead,
	}, nil
}

func (b *readBarrierBackend) CompareAndSwap(
	ctx context.Context,
	snapshot storage.Snapshot,
	req storage.UpdateRequest,
) error {
	barrierSnapshot, ok := snapshot.(*readBarrierSnapshot)
	if !ok {
		return storage.ErrInvalidSnapshot
	}
	return b.Backend.CompareAndSwap(ctx, barrierSnapshot.Snapshot, req)
}

func (b *readBarrierBackend) afterRead(key string) {
	b.mu.Lock()
	if key != b.key || b.remaining == 0 {
		b.mu.Unlock()
		return
	}

	b.remaining--
	ready := b.ready
	if b.remaining == 0 {
		close(ready)
	}
	b.mu.Unlock()

	<-ready
}

type readBarrierSnapshot struct {
	storage.Snapshot
	afterRead func(string)
}

func (s *readBarrierSnapshot) Get(
	ctx context.Context,
	key string,
	dst any,
) error {
	err := s.Snapshot.Get(ctx, key, dst)
	s.afterRead(key)
	return err
}
