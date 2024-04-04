// Package state defines and sores the state for gs.
package state

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path"

	"github.com/rs/zerolog"
	"go.abhg.dev/gs/internal/git"
)

const (
	_repoJSON    = "repo"
	_branchesDir = "branches"
)

var ErrNotExist = os.ErrNotExist

// Store implements storage for gs state inside a Git repository.
type Store struct {
	b     storageBackend
	trunk string
}

func (s *Store) Trunk() string {
	return s.trunk
}

type InitStoreRequest struct {
	// Repository is the Git repository in which to store the state.
	Repository GitRepository

	// Trunk is the name of the trunk branch,
	// e.g. "main" or "master".
	Trunk string

	Log *zerolog.Logger
}

type repoInfo struct {
	Trunk string `json:"trunk"`
}

func InitStore(ctx context.Context, req InitStoreRequest) (*Store, error) {
	log := req.Log
	if log == nil {
		nop := zerolog.Nop()
		log = &nop
	}

	if req.Trunk == "" {
		return nil, errors.New("trunk branch name is required")
	}

	b := newGitStorageBackend(req.Repository, log)
	info := repoInfo{Trunk: req.Trunk}
	if err := b.Put(ctx, _repoJSON, info, "initialize store"); err != nil {
		return nil, fmt.Errorf("put repo state: %w", err)
	}

	return &Store{
		b:     b,
		trunk: req.Trunk,
	}, nil
}

var ErrUninitialized = errors.New("store not initialized")

// OpenStore opens the Store for the given Git repository.
// The store will be created if it does not exist.
func OpenStore(ctx context.Context, repo GitRepository, log *zerolog.Logger) (*Store, error) {
	if log == nil {
		nop := zerolog.Nop()
		log = &nop
	}
	b := newGitStorageBackend(repo, log)

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

type GetBranchResponse struct {
	Base     string
	BaseHash git.Hash
	PR       int
}

// GetBranch returns information about a branch tracked by gs.
// If the branch is not found, [ErrNotExist] will be returned.
func (s *Store) GetBranch(ctx context.Context, name string) (GetBranchResponse, error) {
	var state branchState
	if err := s.b.Get(ctx, s.branchJSON(name), &state); err != nil {
		return GetBranchResponse{}, fmt.Errorf("get branch state: %w", err)
	}

	return GetBranchResponse{
		Base:     state.Base.Name,
		BaseHash: git.Hash(state.Base.Hash),
		PR:       state.PR,
	}, nil
}

type UpsertBranchRequest struct {
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
	// Leave nil to keep the current PR.
	PR *int

	// Message is the message to use for the update.
	// If empty, a message will be generated.
	Message string
}

func PR(n int) *int {
	return &n
}

func (s *Store) UpsertBranch(ctx context.Context, req UpsertBranchRequest) error {
	if req.Name == "" {
		return errors.New("branch name is required")
	}
	if req.Name == s.trunk {
		return errors.New("trunk branch is not managed by gs")
	}

	var b branchState
	if prev, err := s.GetBranch(ctx, req.Name); err != nil {
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
	if req.Message == "" {
		req.Message = fmt.Sprintf("update branch %q", req.Name)
	}

	if b.Base.Name == "" {
		return fmt.Errorf("branch %q would have no base", req.Name)
	}

	if err := s.b.Put(ctx, s.branchJSON(req.Name), b, req.Message); err != nil {
		return fmt.Errorf("put branch state: %w", err)
	}

	return nil
}

func (s *Store) ForgetBranch(ctx context.Context, name string) error {
	err := s.b.Del(ctx, s.branchJSON(name), fmt.Sprintf("forget branch %q", name))
	if err != nil {
		return fmt.Errorf("delete branch: %w", err)
	}
	return nil
}

// UpstackDirect lists branches that are immediately upstack from the given branch.
func (s *Store) UpstackDirect(ctx context.Context, parent string) ([]string, error) {
	branchFiles, err := s.b.Keys(ctx, _branchesDir)
	if err != nil {
		return nil, fmt.Errorf("list branches: %w", err)
	}

	var (
		children []string
		buff     bytes.Buffer
	)
	for branchName := range branchFiles {
		buff.Reset()
		key := path.Join(_branchesDir, branchName)

		var branch branchState
		if err := s.b.Get(ctx, key, &branch); err != nil {
			return nil, fmt.Errorf("get branch state: %w", err)
		}

		if branch.Base.Name == parent {
			children = append(children, branchName)
		}
	}

	return children, nil
}
