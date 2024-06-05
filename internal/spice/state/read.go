package state

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"go.abhg.dev/gs/internal/git"
)

// Trunk reports the trunk branch configured for the repository.
func (s *Store) Trunk() string {
	return s.trunk
}

// Remote returns the remote configured for the repository.
// Returns [ErrNotExist] if no remote is configured.
func (s *Store) Remote() (string, error) {
	if s.remote == "" {
		return "", ErrNotExist
	}

	return s.remote, nil
}

// ErrNotExist indicates that a key that was expected to exist does not exist.
var ErrNotExist = errors.New("does not exist in store")

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

	// UpstreamBranch is the name of the upstream branch
	// or an empty string if the branch is not tracking an upstream branch.
	UpstreamBranch string
}

// LookupBranch returns information about a tracked branch.
// If the branch is not found, [ErrNotExist] will be returned.
func (s *Store) LookupBranch(ctx context.Context, name string) (*LookupResponse, error) {
	state, err := s.lookupBranchState(ctx, name)
	if err != nil {
		return nil, err
	}

	res := &LookupResponse{
		Base:     state.Base.Name,
		BaseHash: git.Hash(state.Base.Hash),
	}
	if gh := state.GitHub; gh != nil {
		res.PR = gh.PR
	}
	if upstream := state.Upstream; upstream != nil {
		res.UpstreamBranch = upstream.Branch
	}

	return res, nil
}

func (s *Store) lookupBranchState(ctx context.Context, name string) (*branchState, error) {
	var state branchState
	if err := s.b.Get(ctx, s.branchJSON(name), &state); err != nil {
		return nil, fmt.Errorf("get branch state: %w", err)
	}
	return &state, nil
}

// ListBranches reports the names of all tracked branches.
// The list is sorted in lexicographic order.
func (s *Store) ListBranches(ctx context.Context) ([]string, error) {
	branches, err := s.b.Keys(ctx, _branchesDir)
	if err != nil {
		return nil, fmt.Errorf("list branches: %w", err)
	}
	sort.Strings(branches)
	return branches, nil
}
