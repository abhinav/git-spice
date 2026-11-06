package spice

import (
	"context"
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/must"
)

// ListAbove returns a list of branches that are immediately above the given branch.
// These are branches that have the given branch as their base.
// The slice is empty if there are no branches above the given branch.
func (s *Service) ListAbove(ctx context.Context, base string) ([]string, error) {
	graph, err := s.BranchGraph(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("get branch graph: %w", err)
	}

	return slices.Collect(graph.Aboves(base)), nil
}

// ListUpstack will list all branches that are upstack from the given branch,
// including those that are upstack from the upstack branches.
// The given branch is the first element in the returned slice.
//
// The returned slice is ordered by branch position in the upstack.
// It is guaranteed that for i < j, branch[i] is not a parent of branch[j].
func (s *Service) ListUpstack(ctx context.Context, start string) ([]string, error) {
	graph, err := s.BranchGraph(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("get branch graph: %w", err)
	}

	upstacks := slices.Collect(graph.Upstack(start))
	if len(upstacks) == 0 {
		// Empty iterator means the branch is not tracked.
		// TODO: should we return an error here?
		upstacks = []string{start}
	}
	must.NotBeEmptyf(upstacks, "there must be at least one branch")
	must.BeEqualf(start, upstacks[0], "starting branch must be first upstack")
	return upstacks, nil
}

// FindTop returns the topmost branches in each upstack chain
// starting at the given branch.
func (s *Service) FindTop(ctx context.Context, start string) ([]string, error) {
	graph, err := s.BranchGraph(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("get branch graph: %w", err)
	}

	return slices.Collect(graph.Tops(start)), nil
}

// ListDownstack lists all branches below the given branch
// in the downstack chain, not including trunk.
//
// The given branch is the first element in the returned slice,
// and the bottom-most branch is the last element.
//
// If there are no branches downstack because we're on trunk,
// or because all branches are downstack from trunk have been deleted,
// the returned slice will be nil.
func (s *Service) ListDownstack(ctx context.Context, start string) ([]string, error) {
	graph, err := s.BranchGraph(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("get branch graph: %w", err)
	}

	downstack := slices.Collect(graph.Downstack(start))
	if len(downstack) == 0 {
		return nil, nil // no downstack branches
	}
	must.BeEqualf(start, downstack[0], "starting branch must be first in downstack")
	return downstack, nil
}

// FindBottom returns the bottom-most branch in the downstack chain
// starting at the given branch just before trunk.
//
// Returns an error if no downstack branches are found.
func (s *Service) FindBottom(ctx context.Context, start string) (string, error) {
	graph, err := s.BranchGraph(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("get branch graph: %w", err)
	}

	bottom := graph.Bottom(start)
	if bottom == "" {
		return "", fmt.Errorf("no downstack branches found for %q", start)
	}
	return bottom, nil
}

// ListStack returns the full stack of branches that the given branch is in.
//
// If the start branch has multiple upstack branches,
// all of them are included in the returned slice.
// The result is ordered by branch position in the stack
// with the bottom-most branch as the first element.
func (s *Service) ListStack(ctx context.Context, start string) ([]string, error) {
	graph, err := s.BranchGraph(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("get branch graph: %w", err)
	}

	stack := slices.Collect(graph.Stack(start))
	if len(stack) == 0 {
		return []string{start}, nil
	}
	return stack, nil
}

// ListStackLinear returns the full stack of branches that the given branch is in
// but only if the stack is linear: each branch has only one upstack branch.
// If the stack is not linear, [NonLinearStackError] is returned.
//
// The returned slice is ordered by branch position in the stack
// with the bottom-most branch as the first element.
func (s *Service) ListStackLinear(ctx context.Context, start string) ([]string, error) {
	graph, err := s.BranchGraph(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("get branch graph: %w", err)
	}

	return graph.StackLinear(start)
}
