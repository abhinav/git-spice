package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// MemBackend is an in-memory storage backend.
type MemBackend struct {
	mu    sync.RWMutex
	items map[string][]byte
}

var _ Backend = (*MemBackend)(nil)

// NewMemBackend creates a new MemBackend.
func NewMemBackend() *MemBackend {
	return &MemBackend{
		items: make(map[string][]byte),
	}
}

// Get retrieves a value from the store
func (m *MemBackend) Get(ctx context.Context, key string, dst any) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	v, ok := m.items[key]
	if !ok {
		return ErrNotExist
	}

	return json.Unmarshal(v, dst)
}

// Update applies a batch of changes to the store.
func (m *MemBackend) Update(ctx context.Context, req UpdateRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, set := range req.Sets {
		v, err := json.Marshal(set.Value)
		if err != nil {
			return fmt.Errorf("marshal [%d]: %w", i, err)
		}
		m.items[set.Key] = v
	}

	for _, key := range req.Deletes {
		delete(m.items, key)
	}

	return nil
}

// Clear clears all keys in the store.
func (m *MemBackend) Clear(context.Context, string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	clear(m.items)
	return nil
}

// Keys returns a list of keys in the store.
func (m *MemBackend) Keys(ctx context.Context, dir string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if dir != "" && !strings.HasSuffix(dir, "/") {
		dir += "/"
	}

	keys := make([]string, 0, len(m.items))
	for k := range m.items {
		if strings.HasPrefix(k, dir) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}
