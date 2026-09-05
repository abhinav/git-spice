package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	json "encoding/json/v2"
	"fmt"
	"sort"
	"strings"
)

// MapBackend is an in-memory [Backend] backed by a map.
//
// It is not thread-safe. Use [SyncBackend] when sharing it.
type MapBackend map[string][]byte

var _ Backend = MapBackend(nil)

// Get retrieves a value from the store.
func (m MapBackend) Get(
	_ context.Context,
	key string,
	dst any,
) error {
	return getFromMap(m, key, dst)
}

// Snapshot returns a read-only copy of the current map contents.
func (m MapBackend) Snapshot(context.Context) (Snapshot, error) {
	data := make(MapBackend, len(m))
	for key, value := range m {
		data[key] = bytes.Clone(value)
	}
	return &mapSnapshot{
		data:        data,
		fingerprint: m.fingerprint(),
	}, nil
}

// CompareAndSwap applies req if snapshot still describes the current map.
func (m MapBackend) CompareAndSwap(
	ctx context.Context,
	snapshot Snapshot,
	req UpdateRequest,
) error {
	snap, ok := snapshot.(*mapSnapshot)
	if !ok {
		return ErrInvalidSnapshot
	}
	if m.fingerprint() != snap.fingerprint {
		return ErrConflict
	}
	return m.Update(ctx, req)
}

// Update applies a batch of changes to the store.
// MapBackend ignores the update message.
func (m MapBackend) Update(
	_ context.Context,
	req UpdateRequest,
) error {
	values := make([][]byte, len(req.Sets))
	for i, set := range req.Sets {
		value, err := json.Marshal(set.Value)
		if err != nil {
			return fmt.Errorf("marshal [%d]: %w", i, err)
		}
		values[i] = value
	}

	for i, set := range req.Sets {
		m[set.Key] = values[i]
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
func (m MapBackend) Keys(
	_ context.Context,
	dir string,
) ([]string, error) {
	return keysFromMap(m, dir), nil
}

func getFromMap(m MapBackend, key string, dst any) error {
	value, ok := m[key]
	if !ok {
		return ErrNotExist
	}
	return json.Unmarshal(value, dst)
}

func keysFromMap(m MapBackend, dir string) []string {
	if dir != "" && !strings.HasSuffix(dir, "/") {
		dir += "/"
	}

	keys := make([]string, 0, len(m))
	for key := range m {
		if rest, ok := strings.CutPrefix(key, dir); ok {
			keys = append(keys, rest)
		}
	}
	sort.Strings(keys)
	return keys
}

func (m MapBackend) fingerprint() [sha256.Size]byte {
	items := make([]mapFingerprintItem, 0, len(m))
	for key, value := range m {
		items = append(items, mapFingerprintItem{
			Key:   key,
			Value: value,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Key < items[j].Key
	})

	data, err := json.Marshal(items)
	if err != nil {
		panic(fmt.Sprintf("marshal map fingerprint: %v", err))
	}
	return sha256.Sum256(data)
}

type mapSnapshot struct {
	data        MapBackend
	fingerprint [sha256.Size]byte
}

var _ Snapshot = (*mapSnapshot)(nil)

func (s *mapSnapshot) Get(
	_ context.Context,
	key string,
	dst any,
) error {
	return getFromMap(s.data, key, dst)
}

func (s *mapSnapshot) Keys(
	_ context.Context,
	dir string,
) ([]string, error) {
	return keysFromMap(s.data, dir), nil
}

type mapFingerprintItem struct {
	Key   string `json:"key"`
	Value []byte `json:"value"`
}
