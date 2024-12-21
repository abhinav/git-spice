package storage

import (
	"context"
	"sync"
)

// SyncBackend adds mutex-based synchronization around all operations
// of the given backend to make it thread-safe.
//
// Use it with [MapBackend].
func SyncBackend(b Backend) Backend {
	return &syncBackend{b: b}
}

type syncBackend struct {
	mu sync.RWMutex
	b  Backend
}

var _ Backend = (*syncBackend)(nil)

func (s *syncBackend) Clear(ctx context.Context, msg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.b.Clear(ctx, msg)
}

func (s *syncBackend) Get(ctx context.Context, key string, dst any) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.b.Get(ctx, key, dst)
}

func (s *syncBackend) Keys(ctx context.Context, dir string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.b.Keys(ctx, dir)
}

func (s *syncBackend) Update(ctx context.Context, req UpdateRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.b.Update(ctx, req)
}
