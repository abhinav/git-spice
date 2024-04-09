// Package state defines and sores the state for gs.
package state

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"path"
	"strings"

	"github.com/charmbracelet/log"
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
	log   *log.Logger
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

	Log *log.Logger

	// Force will clear the store if it's already initialized.
	// Without this, InitStore will fail with ErrAlreadyInitialized.
	Force bool
}

type repoInfo struct {
	Trunk string `json:"trunk"`
}

var ErrAlreadyInitialized = errors.New("store already initialized")

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

	info := repoInfo{Trunk: req.Trunk}
	if err := b.Put(ctx, _repoJSON, info, "initialize store"); err != nil {
		return nil, fmt.Errorf("put repo state: %w", err)
	}

	return &Store{
		b:     b,
		trunk: req.Trunk,
		log:   logger,
	}, nil
}

var ErrUninitialized = errors.New("store not initialized")

// OpenStore opens the Store for the given Git repository.
// The store will be created if it does not exist.
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

func (s *branchState) toBranch(name string) *Branch {
	return &Branch{
		Name: name,
		Base: &BranchBase{
			Name: s.Base.Name,
			Hash: git.Hash(s.Base.Hash),
		},
		PR: s.PR,
	}
}

type BranchBase struct {
	Name string
	Hash git.Hash
}

func (b BranchBase) String() string {
	return fmt.Sprintf("%s@%s", b.Name, b.Hash)
}

type Branch struct {
	Name string
	Base *BranchBase
	PR   int
}

func (b *Branch) String() string {
	var s strings.Builder
	s.WriteString(b.Name)
	if b.PR != 0 {
		fmt.Fprintf(&s, " (#%d)", b.PR)
	}
	if b.Base != nil {
		fmt.Fprintf(&s, " (base: %v)", b.Base)
	}
	return s.String()
}

// LookupBranch returns information about a branch tracked by gs.
// If the branch is not found, [ErrNotExist] will be returned.
func (s *Store) LookupBranch(ctx context.Context, name string) (*Branch, error) {
	var state branchState
	if err := s.b.Get(ctx, s.branchJSON(name), &state); err != nil {
		return nil, fmt.Errorf("get branch state: %w", err)
	}

	return state.toBranch(name), nil
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
	if prev, err := s.LookupBranch(ctx, req.Name); err != nil {
		if !errors.Is(err, ErrNotExist) {
			return fmt.Errorf("get branch: %w", err)
		}
		// Branch does not exist yet.
		// Everything is already set to the zero value.
	} else {
		b.PR = prev.PR
		b.Base = branchStateBase{
			Name: prev.Base.Name,
			Hash: prev.Base.Hash.String(),
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

func (s *Store) allBranches(ctx context.Context) iter.Seq2[*Branch, error] {
	return func(yield func(*Branch, error) bool) {
		branchNames, err := s.b.Keys(ctx, _branchesDir)
		if err != nil {
			yield(nil, fmt.Errorf("list branches: %w", err))
			return
		}

		for branchName := range branchNames {
			var branch branchState
			if err := s.b.Get(ctx, path.Join(_branchesDir, branchName), &branch); err != nil {
				yield(nil, fmt.Errorf("get branch state: %w", err))
				break
			}

			if !yield(branch.toBranch(branchName), nil) {
				break
			}
		}
	}
}

// ListAbove lists branches that are immediately upstack from the given branch.
func (s *Store) ListAbove(ctx context.Context, base string) ([]string, error) {
	var children []string
	for branch, err := range s.allBranches(ctx) {
		if err != nil {
			return nil, err
		}

		if branch.Base.Name == base {
			children = append(children, branch.Name)
		}
	}

	return children, nil
}

// ListUpstack will list all branches that are upstack from the given branch,
// with the given branch as the starting point.
//
// The returned slice is ordered by branch position in the upstack.
// Earlier elements are closer to the trunk.
func (s *Store) ListUpstack(ctx context.Context, start string) ([]string, error) {
	branchesByBase := make(map[string][]string) // base name -> branches on base
	for branch, err := range s.allBranches(ctx) {
		if err != nil {
			return nil, err
		}

		branchesByBase[branch.Base.Name] = append(
			branchesByBase[branch.Base.Name], branch.Name,
		)
	}

	var upstacks []string
	remaining := []string{start}
	for len(remaining) > 0 {
		current := remaining[0]
		remaining = remaining[1:]
		upstacks = append(upstacks, current)
		remaining = append(remaining, branchesByBase[current]...)
	}

	return upstacks, nil
}
