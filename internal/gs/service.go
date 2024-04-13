// Package gs intends to provide the core functionality of the gs tool.
package gs

import (
	"context"
	"fmt"
	"iter"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/state"
)

// GitRepository provides read/write access to the conents of a git repository.
// It is a subset of the functionality provied by the git.Repository type.
type GitRepository interface {
	// TODO
}

var _ GitRepository = (*git.Repository)(nil)

// BranchStore provides storage for branch state for gs.
//
// It is a subset of the functionality provided by the state.Store type.
type BranchStore interface {
	// Lookup returns the branch state for the given branch,
	// or [state.ErrNotExist] if the branch does not exist.
	Lookup(ctx context.Context, name string) (*state.LookupResponse, error)

	// Update adds, updates, or removes state information
	// for zero or more branches.
	Update(ctx context.Context, req *state.UpdateRequest) error

	// List returns a list of all tracked branch names.
	// This list never includes the trunk branch.
	List(ctx context.Context) ([]string, error)

	// Trunk returns the name of the trunk branch.
	Trunk() string
}

var _ BranchStore = (*state.Store)(nil)

// Service provides the core functionality of the gs tool.
type Service struct {
	repo  GitRepository
	store BranchStore
	log   *log.Logger
}

// NewService builds a new service operating on the given repository and store.
func NewService(repo GitRepository, store BranchStore, log *log.Logger) *Service {
	return &Service{
		repo:  repo,
		store: store,
		log:   log,
	}
}

type branchInfo struct {
	*state.LookupResponse
	Name string
}

func (s *Service) allBranches(ctx context.Context) iter.Seq2[branchInfo, error] {
	names, err := s.store.List(ctx)
	if err != nil {
		return func(yield func(branchInfo, error) bool) {
			yield(branchInfo{}, fmt.Errorf("list branches: %w", err))
		}
	}

	return func(yield func(branchInfo, error) bool) {
		for _, name := range names {
			resp, err := s.store.Lookup(ctx, name)
			if err != nil {
				yield(branchInfo{}, fmt.Errorf("get branch %v: %w", name, err))
				break
			}

			info := branchInfo{
				Name:           name,
				LookupResponse: resp,
			}
			if !yield(info, nil) {
				break
			}
		}
	}
}

func (s *Service) branchesByBase(ctx context.Context) (map[string][]string, error) {
	branchesByBase := make(map[string][]string)
	for branch, err := range s.allBranches(ctx) {
		if err != nil {
			return nil, err
		}

		branchesByBase[branch.Base] = append(
			branchesByBase[branch.Base], branch.Name,
		)
	}
	return branchesByBase, nil
}

// ListAbove returns a list of branches that are immediately above the given branch.
// These are branches that have the given branch as their base.
// The slice is empty if there are no branches above the given branch.
func (s *Service) ListAbove(ctx context.Context, base string) ([]string, error) {
	var children []string
	for branch, err := range s.allBranches(ctx) {
		if err != nil {
			return nil, err
		}

		if branch.Base == base {
			children = append(children, branch.Name)
		}
	}

	return children, nil
}

// ListUpstack will list all branches that are upstack from the given branch,
// including those that are upstack from the upstack branches.
// The given branch is the first element in the returned slice.
//
// The returned slice is ordered by branch position in the upstack.
// It is guaranteed that for i < j, branch[i] is not a parent of branch[j].
func (s *Service) ListUpstack(ctx context.Context, start string) ([]string, error) {
	branchesByBase, err := s.branchesByBase(ctx) // base -> [branches]
	if err != nil {
		return nil, err
	}

	var upstacks []string
	remaining := []string{start}
	for len(remaining) > 0 {
		current := remaining[0]
		remaining = remaining[1:]
		upstacks = append(upstacks, current)
		remaining = append(remaining, branchesByBase[current]...)
	}
	must.NotBeEmptyf(upstacks, "there must be at least one branch")
	must.BeEqualf(start, upstacks[0], "starting branch must be first upstack")

	return upstacks, nil
}

// FindTop returns the topmost branches in each upstack chain
// starting at the given branch.
func (s *Service) FindTop(ctx context.Context, start string) ([]string, error) {
	branchesByBase, err := s.branchesByBase(ctx) // base -> [branches]
	if err != nil {
		return nil, err
	}

	remaining := []string{start}
	var tops []string
	for len(remaining) > 0 {
		var b string
		b, remaining = remaining[0], remaining[1:]

		aboves := branchesByBase[b]
		if len(aboves) == 0 {
			// There's nothing above this branch
			// so it's a top-most branch.
			tops = append(tops, b)
		} else {
			remaining = append(remaining, aboves...)
		}
	}
	must.NotBeEmptyf(tops, "at least start branch (%v) must be in tops", start)
	return tops, nil
}

// FindBottom returns the bottom-most branch in the downstack chain
// starting at the given branch just before trunk.
func (s *Service) FindBottom(ctx context.Context, start string) (string, error) {
	must.NotBeEqualf(start, s.store.Trunk(), "start branch must not be trunk")

	current := start
	for {
		b, err := s.store.Lookup(ctx, current)
		if err != nil {
			return "", fmt.Errorf("lookup %v: %w", current, err)
		}

		if b.Base == s.store.Trunk() {
			return current, nil
		}

		current = b.Base
	}
}
