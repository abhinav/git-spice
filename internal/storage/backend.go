// Package storage provides a key-value storage abstraction
// where values are JSON-serializable structs.
// It is used by git-spice to store metadata in a git repository.
// It requires all write operations to have a message.
package storage

import (
	"context"
	"errors"
)

// UpdateRequest performs a batch of write operations
// in one transaction.
type UpdateRequest struct {
	Sets []SetRequest

	// Deletes lists the keys to delete.
	Deletes []string

	// Message to attach to the batch oepration.
	Message string
}

// SetRequest is a single operation to add or update a key.
type SetRequest struct {
	// Key to write to.
	Key string

	// Value to serialize to JSON.
	Value any
}

// ErrNotExist indicates that a key that was expected to exist does not exist.
var ErrNotExist = errors.New("does not exist in store")

// Backend defines the primitive operations for the key-value store.
type Backend interface {
	// Get retrieves a value from the store
	// and decodes it into dst.
	//
	// If the key does not exist, Get returns ErrNotExist.
	Get(ctx context.Context, key string, dst any) error

	Update(ctx context.Context, req UpdateRequest) error
	Clear(ctx context.Context, msg string) error

	// Keys lists the keys in the store in the given directory,
	// with the directory prefix removed.
	//
	// The directory is defined as '/'-separated components in the key.
	// If dir is empty, all keys are listed.
	Keys(ctx context.Context, dir string) ([]string, error)
}

// DB is a high-level wrapper around a Backend.
// It provides a more convenient API for interacting with the store.
type DB struct{ Backend }

// NewDB creates a new DB using the given Backend.
func NewDB(b Backend) *DB {
	return &DB{Backend: b}
}

// Set adds or updates a single key in the store.
func (db *DB) Set(ctx context.Context, key string, value any, msg string) error {
	return db.Update(ctx, UpdateRequest{
		Sets:    []SetRequest{{Key: key, Value: value}},
		Message: msg,
	})
}

// Delete removes a key from the store.
func (db *DB) Delete(ctx context.Context, key string, msg string) error {
	return db.Update(ctx, UpdateRequest{
		Deletes: []string{key},
		Message: msg,
	})
}
