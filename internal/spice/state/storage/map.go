package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// MapBackend is an in-memory implementation of [Backend] backed by a map.
//
// This is NOT thread safe. Use [SyncBackend] to make it so.
type MapBackend map[string][]byte

var _ Backend = (MapBackend)(nil)

// Get retrieves a value from the store.
func (m MapBackend) Get(ctx context.Context, key string, dst any) error {
	v, ok := m[key]
	if !ok {
		return ErrNotExist
	}

	return json.Unmarshal(v, dst)
}

// Update applies a batch of changes to the store.
// MapBackend ignores the message associated with the update.
func (m MapBackend) Update(ctx context.Context, req UpdateRequest) error {
	for i, set := range req.Sets {
		v, err := json.Marshal(set.Value)
		if err != nil {
			return fmt.Errorf("marshal [%d]: %w", i, err)
		}
		m[set.Key] = v
	}

	for _, key := range req.Deletes {
		delete(m, key)
	}

	return nil
}

// Clear clears all keys in the store.
func (m MapBackend) Clear(context.Context, string) error {
	clear(m)
	return nil
}

// Keys returns a list of keys in the store.
func (m MapBackend) Keys(ctx context.Context, dir string) ([]string, error) {
	if dir != "" && !strings.HasSuffix(dir, "/") {
		dir += "/"
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		if rest, ok := strings.CutPrefix(k, dir); ok {
			keys = append(keys, rest)
		}
	}
	sort.Strings(keys)
	return keys, nil
}
