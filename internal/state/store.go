// Package state defines and sores the state for gs.
package state

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"time"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
)

const (
	_repoJSON    = "repo"
	_branchesDir = "branches"
)

// ErrNotExist indicates that a key that was expected to exist does not exist.
var ErrNotExist = os.ErrNotExist

// Store implements storage for gs state inside a Git repository.
type Store struct {
	b     storageBackend
	trunk string
	log   *log.Logger
}

// Trunk reports the trunk branch configured for the repository.
func (s *Store) Trunk() string {
	return s.trunk
}

// InitStoreRequest is a request to initialize the store
// in a Git repository.
type InitStoreRequest struct {
	// Repository is the Git repository being initialized.
	// State will be stored in a ref in this repository.
	Repository GitRepository

	// Trunk is the name of the trunk branch,
	// e.g. "main" or "master".
	Trunk string

	// Force will clear the store if it's already initialized.
	// Without this, InitStore will fail with ErrAlreadyInitialized.
	Force bool

	// Log is the logger to use for logging.
	Log *log.Logger
}

type repoInfo struct {
	Trunk string `json:"trunk"`
}

// ErrAlreadyInitialized indicates that the store is already initialized.
var ErrAlreadyInitialized = errors.New("store already initialized")

// InitStore initializes the store in the given Git repository.
//
// It returns [ErrAlreadyInitialized] if the repository is already initialized
// and Force is not set.
func InitStore(ctx context.Context, req InitStoreRequest) (*Store, error) {
	logger := req.Log
	if logger == nil {
		logger = log.New(io.Discard)
	}

	if req.Trunk == "" {
		return nil, errors.New("trunk branch name is required")
	}

	b := newGitStorageBackend(req.Repository, logger)
	if err := b.Get(ctx, _repoJSON, new(repoInfo)); err == nil {
		if !req.Force {
			return nil, ErrAlreadyInitialized
		}
		if err := b.Clear(ctx, "re-initializing store"); err != nil {
			return nil, fmt.Errorf("clear store: %w", err)
		}
	}

	err := b.Update(ctx, updateRequest{
		Sets: []setRequest{
			{
				Key: _repoJSON,
				Val: repoInfo{Trunk: req.Trunk},
			},
		},
		Msg: "initialize store",
	})
	if err != nil {
		return nil, fmt.Errorf("put repo state: %w", err)
	}

	return &Store{
		b:     b,
		trunk: req.Trunk,
		log:   logger,
	}, nil
}

// ErrUninitialized indicates that the store is not initialized.
var ErrUninitialized = errors.New("store not initialized")

// OpenStore opens the Store for the given Git repository.
//
// It returns [ErrUninitialized] if the repository is not initialized.
func OpenStore(ctx context.Context, repo GitRepository, logger *log.Logger) (*Store, error) {
	if logger == nil {
		logger = log.New(io.Discard)
	}
	b := newGitStorageBackend(repo, logger)

	var info repoInfo
	if err := b.Get(ctx, _repoJSON, &info); err != nil {
		if errors.Is(err, ErrNotExist) {
			return nil, ErrUninitialized
		}
		return nil, fmt.Errorf("get repo state: %w", err)
	}

	return &Store{
		b:     b,
		trunk: info.Trunk,
		log:   logger,
	}, nil
}

func (s *Store) branchJSON(name string) string {
	return path.Join(_branchesDir, name)
}

type branchStateBase struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

type branchState struct {
	Base branchStateBase `json:"base"`
	PR   int             `json:"pr,omitempty"`
}

// LookupResponse is the response to a Lookup request.
type LookupResponse struct {
	// Base is the base branch configured
	// for the requested branch.
	Base string

	// BaseHash is the last known hash of the base branch.
	// This may not match the current hash of the base branch.
	BaseHash git.Hash

	// PR is the number of the pull request associated with the branch,
	// or zero if the branch is not associated with a PR.
	PR int
}

// Lookup returns information about a branch tracked by gs.
// If the branch is not found, [ErrNotExist] will be returned.
func (s *Store) Lookup(ctx context.Context, name string) (*LookupResponse, error) {
	var state branchState
	if err := s.b.Get(ctx, s.branchJSON(name), &state); err != nil {
		return nil, fmt.Errorf("get branch state: %w", err)
	}

	return &LookupResponse{
		Base:     state.Base.Name,
		BaseHash: git.Hash(state.Base.Hash),
		PR:       state.PR,
	}, nil
}

// List reports the names of all tracked branches.
// The list is sorted in lexicographic order.
func (s *Store) List(ctx context.Context) ([]string, error) {
	branches, err := s.b.Keys(ctx, _branchesDir)
	if err != nil {
		return nil, fmt.Errorf("list branches: %w", err)
	}
	sort.Strings(branches)
	return branches, nil
}

// UpsertRequest is a request to add or update information about a branch.
type UpsertRequest struct {
	// Name is the name of the branch.
	Name string

	// Base branch to update to.
	//
	// Leave empty to keep the current base.
	Base string

	// BaseHash is the last known hash of the base branch.
	// This is used to detect if the base branch has been updated.
	//
	// Leave empty to keep the current base hash.
	BaseHash git.Hash

	// PR is the number of the pull request associated with the branch.
	// Zero if the branch is not associated with a PR yet.
	//
	// Leave nil to keep the current PR.
	PR *int
}

// PR is a helper to create a pointer to an integer
// for the UpsertRequest PR field.
func PR(n int) *int {
	return &n
}

// UpdateRequest is a request to add, update, or delete information about branches.
type UpdateRequest struct {
	// Upserts are requests to add or update information about branches.
	Upserts []UpsertRequest

	// Deletes are requests to delete information about branches.
	Deletes []string

	// Message is a message specifying the reason for the update.
	// This will be persisted in the Git commit message.
	Message string
}

// Update upates the store with the parameters in the request.
func (s *Store) Update(ctx context.Context, req *UpdateRequest) error {
	if req.Message == "" {
		req.Message = fmt.Sprintf("update at %s", time.Now().Format(time.RFC3339))
	}

	sets := make([]setRequest, len(req.Upserts))
	for i, req := range req.Upserts {
		if req.Name == "" {
			return fmt.Errorf("upsert [%d]: branch name is required", i)
		}
		if req.Name == s.trunk {
			return fmt.Errorf("upsert [%d]: trunk branch is not managed by gs", i)
		}

		var b branchState
		if prev, err := s.Lookup(ctx, req.Name); err != nil {
			if !errors.Is(err, ErrNotExist) {
				return fmt.Errorf("get branch: %w", err)
			}
			// Branch does not exist yet.
			// Everything is already set to the zero value.
		} else {
			b.PR = prev.PR
			b.Base = branchStateBase{
				Name: prev.Base,
				Hash: prev.BaseHash.String(),
			}
		}

		if req.Base != "" {
			b.Base.Name = req.Base
		}
		if req.BaseHash != "" {
			b.Base.Hash = req.BaseHash.String()
		}
		if req.PR != nil {
			b.PR = *req.PR
		}

		if b.Base.Name == "" {
			return fmt.Errorf("branch %q (%d) would have no base", req.Name, i)
		}

		sets = append(sets, setRequest{
			Key: s.branchJSON(req.Name),
			Val: b,
		})
	}

	deletes := make([]string, len(req.Deletes))
	for i, name := range req.Deletes {
		deletes[i] = s.branchJSON(name)
	}

	err := s.b.Update(ctx, updateRequest{
		Sets: sets,
		Dels: deletes,
		Msg:  req.Message,
	})
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}

	return nil
}
