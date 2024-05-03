package gs

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/state"
)

type branchInfo struct {
	*state.LookupResponse
	Name string
}

func (s *Service) allBranches(ctx context.Context) ([]branchInfo, error) {
	names, err := s.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list branches: %w", err)
	}

	infos := make([]branchInfo, len(names))
	for i, name := range names {
		resp, err := s.store.Lookup(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("get branch %v: %w", name, err)
		}

		infos[i] = branchInfo{
			Name:           name,
			LookupResponse: resp,
		}
	}

	return infos, nil
}

func (s *Service) branchesByBase(ctx context.Context) (map[string][]string, error) {
	branchesByBase := make(map[string][]string)
	branches, err := s.allBranches(ctx)
	if err != nil {
		return nil, err
	}
	for _, branch := range branches {
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
	branches, err := s.allBranches(ctx)
	if err != nil {
		return nil, err
	}
	for _, branch := range branches {
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
