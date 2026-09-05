// Package storage provides a key-value storage abstraction
// where values are JSON-serializable structs.
// It is used by git-spice to store metadata in a Git repository.
// It requires all write operations to have a message.
package storage

import (
	"context"
	"errors"
)

// Backend defines the primitive operations for the key-value store.
type Backend interface {
	// Get retrieves a value from the store and decodes it into dst.
	// If the key does not exist, Get returns [ErrNotExist].
	Get(ctx context.Context, key string, dst any) error

	// Snapshot returns a read-only view of the current store revision.
	Snapshot(ctx context.Context) (Snapshot, error)

	// CompareAndSwap applies req only if snapshot is still current.
	// It returns [ErrConflict] if another writer changed the store first.
	CompareAndSwap(
		ctx context.Context,
		snapshot Snapshot,
		req UpdateRequest,
	) error

	// Update applies req to the current store revision.
	Update(ctx context.Context, req UpdateRequest) error

	// Clear removes every key from the store.
	Clear(ctx context.Context, msg string) error

	// Keys lists the keys in dir with the directory prefix removed.
	// An empty dir lists every key.
	Keys(ctx context.Context, dir string) ([]string, error)
}

// Snapshot is a read-only view of one store revision.
// Its reads remain pinned if the live store changes.
type Snapshot interface {
	// Get retrieves a value from the snapshot and decodes it into dst.
	// If the key does not exist, Get returns [ErrNotExist].
	Get(ctx context.Context, key string, dst any) error

	// Keys lists the keys in dir with the directory prefix removed.
	// An empty dir lists every key.
	Keys(ctx context.Context, dir string) ([]string, error)
}

// UpdateRequest performs a batch of writes in one transaction.
type UpdateRequest struct {
	// Sets lists the keys to add or replace.
	Sets []SetRequest

	// Deletes lists the keys to delete.
	Deletes []string

	// Message is attached to the batch operation.
	Message string
}

// SetRequest is one key-value write.
type SetRequest struct {
	// Key is the key to write.
	Key string

	// Value is serialized to JSON.
	Value any
}

// ErrNotExist indicates that an expected key does not exist.
var ErrNotExist = errors.New("does not exist in store")

// ErrConflict indicates that a compare-and-swap snapshot is stale.
var ErrConflict = errors.New("store changed since snapshot")

// ErrInvalidSnapshot indicates that a snapshot belongs to another backend.
var ErrInvalidSnapshot = errors.New("invalid store snapshot")

// DB is a high-level wrapper around a [Backend].
type DB struct{ Backend }

// NewDB creates a DB using b.
func NewDB(b Backend) *DB {
	return &DB{Backend: b}
}

// Set adds or updates one key.
func (db *DB) Set(
	ctx context.Context,
	key string,
	value any,
	msg string,
) error {
	return db.Update(ctx, UpdateRequest{
		Sets:    []SetRequest{{Key: key, Value: value}},
		Message: msg,
	})
}

// Delete removes one key.
func (db *DB) Delete(
	ctx context.Context,
	key string,
	msg string,
) error {
	return db.Update(ctx, UpdateRequest{
		Deletes: []string{key},
		Message: msg,
	})
}
