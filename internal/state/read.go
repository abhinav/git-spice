package state

import (
	"context"
	"fmt"
	"os"
	"sort"

	"go.abhg.dev/gs/internal/git"
)

// Trunk reports the trunk branch configured for the repository.
func (s *Store) Trunk() string {
	return s.trunk
}

// GitHubRepo reports the GitHub repository associated with the store.
func (s *Store) GitHubRepo() GitHubRepo {
	return s.ghrepo
}

// ErrNotExist indicates that a key that was expected to exist does not exist.
var ErrNotExist = os.ErrNotExist

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
