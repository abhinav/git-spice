// Package state defines and sores the state for gs.
package state

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/charmbracelet/log"
)

// Store implements storage for gs state inside a Git repository.
type Store struct {
	b   storageBackend
	log *log.Logger

	trunk  string
	ghrepo GitHubRepo
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

	// GitHub is the GitHub repository associated with the Git repository.
	GitHub GitHubRepo

	// Force will clear the store if it's already initialized.
	// Without this, InitStore will fail with ErrAlreadyInitialized.
	Force bool

	// Log is the logger to use for logging.
	Log *log.Logger
}

// GitHubRepo contains information about a GitHub repository.
type GitHubRepo struct {
	Owner string
	Name  string
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
				Val: repoInfo{
					Trunk: req.Trunk,
					GitHub: &gitHubInfo{
						Owner: req.GitHub.Owner,
						Name:  req.GitHub.Name,
					},
				},
			},
		},
		Msg: "initialize store",
	})
	if err != nil {
		return nil, fmt.Errorf("put repo state: %w", err)
	}

	return &Store{
		b:      b,
		log:    logger,
		trunk:  req.Trunk,
		ghrepo: req.GitHub,
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

	switch {
	case info.Trunk == "":
		return nil, fmt.Errorf("corrupt state: trunk branch name is empty")
	case info.GitHub == nil:
		return nil, fmt.Errorf("corrupt state: GitHub information not present")
	case info.GitHub.Owner == "":
		return nil, fmt.Errorf("corrupt state: GitHub owner is empty")
	case info.GitHub.Name == "":
		return nil, fmt.Errorf("corrupt state: GitHub repository name is empty")
	}

	return &Store{
		b:      b,
		log:    logger,
		trunk:  info.Trunk,
		ghrepo: GitHubRepo{info.GitHub.Owner, info.GitHub.Name},
	}, nil
}
