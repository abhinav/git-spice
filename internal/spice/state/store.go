// Package state defines and sores the state for gs.
package state

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/storage"
)

// DB provides a key-value store that holds JSON values.
type DB interface {
	Get(ctx context.Context, k string, v any) error
	Keys(ctx context.Context, dir string) ([]string, error)

	Set(ctx context.Context, k string, v any, msg string) error
	Delete(ctx context.Context, k, msg string) error
	Update(ctx context.Context, req storage.UpdateRequest) error
	Clear(ctx context.Context, msg string) error
}

var _ DB = (*storage.DB)(nil)

// Store implements storage for state tracked by gs.
type Store struct {
	db  DB
	log *log.Logger

	trunk  string
	remote string
}

// InitStoreRequest is a request to initialize the store
// in a Git repository.
type InitStoreRequest struct {
	DB DB

	// Trunk is the name of the trunk branch,
	// e.g. "main" or "master".
	Trunk string

	// Remote is the name of the remote to use for pushing and pulling.
	// e.g. "origin" or "upstream".
	//
	// If empty, a remote will not be configured and push/pull
	// operations will not be available.
	Remote string

	// Reset indicates that the store's state should be nuked
	// if it's already initialized.
	Reset bool

	// Log is the logger to use for logging.
	Log *log.Logger
}

// InitStore initializes the store in the given Git repository.
// If the repository is already initialized, it will be re-initialized,
// while retaining existing tracked branches.
// If Reset is true, existing tracked branches will be cleared.
func InitStore(ctx context.Context, req InitStoreRequest) (*Store, error) {
	logger := req.Log
	if logger == nil {
		logger = log.New(io.Discard)
	}

	if req.Trunk == "" {
		return nil, errors.New("trunk branch name is required")
	}

	db := req.DB
	store := &Store{
		db:     db,
		trunk:  req.Trunk,
		remote: req.Remote,
		log:    logger,
	}
	if err := db.Get(ctx, _repoJSON, new(repoInfo)); err == nil {
		if req.Reset {
			if err := db.Clear(ctx, "reset store"); err != nil {
				return nil, fmt.Errorf("clear store: %w", err)
			}
		} else {
			// If we're not resetting,
			// ensure that the trunk branch is not tracked.
			_, err := store.LookupBranch(ctx, req.Trunk)
			if err == nil {
				// TODO: this should all be in 'repo init' implementation.
				return nil, fmt.Errorf("trunk branch (%q) is tracked by gs; use --reset to clear", req.Trunk)
			}
		}
	}

	info := repoInfo{
		Trunk:  req.Trunk,
		Remote: req.Remote,
	}
	if err := db.Set(ctx, _repoJSON, info, "initialize store"); err != nil {
		return nil, fmt.Errorf("put repo state: %w", err)
	}

	return store, nil
}

// ErrUninitialized indicates that the store is not initialized.
var ErrUninitialized = errors.New("store not initialized")

// OpenStore opens the Store for the given Git repository.
//
// It returns [ErrUninitialized] if the repository is not initialized.
func OpenStore(ctx context.Context, db DB, logger *log.Logger) (*Store, error) {
	if logger == nil {
		logger = log.New(io.Discard)
	}

	var info repoInfo
	if err := db.Get(ctx, _repoJSON, &info); err != nil {
		if errors.Is(err, ErrNotExist) {
			return nil, ErrUninitialized
		}
		return nil, fmt.Errorf("get repo state: %w", err)
	}

	if err := info.Validate(); err != nil {
		return nil, fmt.Errorf("corrupt state: %w", err)
	}

	return &Store{
		db:     db,
		trunk:  info.Trunk,
		remote: info.Remote,
		log:    logger,
	}, nil
}
