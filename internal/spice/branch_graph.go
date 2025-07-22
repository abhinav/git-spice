package spice

import (
	"context"
	"fmt"
	"iter"
	"slices"

	"go.abhg.dev/container/ring"
)

// BranchGraph is a full view of the graph of branches in the repository.
type BranchGraph struct {
	trunk    string
	branches []BranchGraphItem // all tracked branches
	byName   map[string]int    // name -> index in branches
	byBase   map[string][]int  // name -> [indices in branches]
}

// BranchGraphItem is a single item in the branch graph.
type BranchGraphItem = LoadBranchItem

// TODO: maybe we kill LoadBranchItem?

// BranchGraphOptions specifies options for the BranchGraph method.
type BranchGraphOptions struct{}

// BranchLoader is a source of branch information in the repository.
type BranchLoader interface {
	Trunk() string

	// LoadBranches loads all branches in the repository.
	LoadBranches(ctx context.Context) ([]LoadBranchItem, error)
}

// NewBranchGraph returns a full view of the graph of branches in the repository.
func NewBranchGraph(ctx context.Context, loader BranchLoader, _ *BranchGraphOptions) (*BranchGraph, error) {
	branches, err := loader.LoadBranches(ctx)
	if err != nil {
		return nil, fmt.Errorf("load branches: %w", err)
	}

	byName := make(map[string]int, len(branches))
	byBase := make(map[string][]int, len(branches))
	for idx, branch := range branches {
		byName[branch.Name] = idx
		byBase[branch.Base] = append(byBase[branch.Base], idx)
	}

	return &BranchGraph{
		trunk:    loader.Trunk(),
		branches: branches,
		byName:   byName,
		byBase:   byBase,
	}, nil
}

// Trunk reports the name of the trunk branch in the repository.
func (g *BranchGraph) Trunk() string {
	return g.trunk
}

// All returns an iterator over all branches in the graph
// with detailed per-branch information.
func (g *BranchGraph) All() iter.Seq[LoadBranchItem] {
	return slices.Values(g.branches)
}

// Aboves returns branches directly above the given branch,
func (g *BranchGraph) Aboves(branch string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, idx := range g.byBase[branch] {
			if !yield(g.branches[idx].Name) {
				return
			}
		}
	}
}

// Upstack returns all branches that are upstack from the given branch:
// branches that are directly above it, those above those branches,
// and so on.
//
// The first element in the returned sequence is always the branch itself.
// The remaining elements are the branches above it, in toplogical order:
// it is guaranteed that a branch seen earlier in the sequence
// does not use a branch seen later as its base.
//
// If branch is trunk, this reports all branches in the repository,
// including trunk itself.
func (g *BranchGraph) Upstack(branch string) iter.Seq[string] {
	return func(yield func(string) bool) {
		// Use a queue to traverse the upstack branches.
		var q ring.Q[string]
		q.Push(branch)
		for !q.Empty() {
			current := q.Pop()
			if !yield(current) {
				return
			}

			// Add all branches above the current branch to the queue.
			for above := range g.Aboves(current) {
				q.Push(above)
			}
		}
	}
}

// Tops returns all topmost branches in the upstack chain
// starting at the given branch.
//
// A topmost branch is a branch that has no branches above it,
// i.e. no other branch has it as a base.
// branch may be trunk.
func (g *BranchGraph) Tops(branch string) iter.Seq[string] {
	return func(yield func(string) bool) {
		var remaining ring.Q[string]
		remaining.Push(branch)
		for !remaining.Empty() {
			branch := remaining.Pop()
			var hasAboves bool
			for above := range g.Aboves(branch) {
				hasAboves = true
				remaining.Push(above)
			}

			if !hasAboves {
				if !yield(branch) {
					return
				}
			}
		}
	}
}

// Downstack returns all branches that are downstack from the given branch:
// branches that are directly below it, those below those branches,
// and so on.
//
// The first element in the returned sequence is always the branch itself,
// followed by remaining branches in the downstack chain
// in reverse topological order:
// it is guaranteed that a branch seen earlier in the sequence
// is not used as a base by a branch seen later.
//
// trunk is never included in the downstack list.
// If the given branch is trunk, the returned sequence is empty.
func (g *BranchGraph) Downstack(branch string) iter.Seq[string] {
	return func(yield func(string) bool) {
		current := branch
		for {
			if current == g.trunk {
				// Reached trunk, stop traversing downstack.
				return
			}

			idx, ok := g.byName[current]
			if !ok {
				// Branch does not exist in the graph.
				return
			}

			if !yield(current) {
				return
			}

			// Move to the base of the current branch.
			current = g.branches[idx].Base
		}
	}
}

// Bottom returns the bottom-most branch in the downstack chain
// of the given branch.
//
// The bottom-most branch is the last branch in the downstack chain,
// i.e. the last branch before trunk.
//
// Returns an empty string if the given branch is trunk.
func (g *BranchGraph) Bottom(branch string) string {
	for idx, ok := g.byName[branch]; ok; idx, ok = g.byName[branch] {
		base := g.branches[idx].Base
		if base == g.trunk {
			return branch
		}
		branch = base
	}

	return ""
}

// Stack returns the full stack of branches that the given branch is in.
//
// This includes all downstack branches and all upstack branches,
// with the given branch itself in the middle.
// Branches are reversed in topological order:
// it is guaranteed that a branch seen earlier in the sequence
// does not use a branch seen later as its base.
//
// The branch itself is always included in the stack,
// but its position is based on the number of downstack branches
// (its distance from the trunk).
func (g *BranchGraph) Stack(branch string) iter.Seq[string] {
	return func(yield func(string) bool) {
		// Downstack is in the reverse order than what we want,
		// so we have to collect it first.
		downstack := slices.Collect(g.Downstack(branch))
		// Drop the branch itself from the downstack.
		if len(downstack) > 0 && downstack[0] == branch {
			downstack = downstack[1:]
		}
		slices.Reverse(downstack)
		for _, b := range downstack {
			if !yield(b) {
				return
			}
		}

		// Upstack branches includes the branch itself
		// as the first element,
		for up := range g.Upstack(branch) {
			if !yield(up) {
				return
			}
		}
	}
}

// NonLinearStackError is returned when a stack is not linear.
// This means that a branch has more than one upstack branch.
type NonLinearStackError struct {
	Branch string
	Aboves []string
}

func (e *NonLinearStackError) Error() string {
	return fmt.Sprintf("%v has %d branches above it", e.Branch, len(e.Aboves))
}

// StackLinear returns the full stack of branches that the given branch is in,
// but only if the stack is linear: each branch has only one upstack branch.
//
// Returns [NonLinearStackError] if the stack is not linear.
func (g *BranchGraph) StackLinear(branch string) ([]string, error) {
	// TODO: probably don't need this on the graph itself
	downstacks := slices.Collect(g.Downstack(branch))
	if len(downstacks) > 0 && downstacks[0] == branch {
		downstacks = downstacks[1:] // drop the branch itself
	}
	slices.Reverse(downstacks)

	upstacks := []string{branch}
	current := branch
	for aboves := g.byBase[current]; len(aboves) > 0; {
		if len(aboves) > 1 {
			aboveNames := make([]string, len(aboves))
			for i, idx := range aboves {
				aboveNames[i] = g.branches[idx].Name
			}

			return nil, &NonLinearStackError{
				Branch: current,
				Aboves: aboveNames,
			}
		}

		above := g.branches[aboves[0]]
		current = above.Name
		upstacks = append(upstacks, current)
		aboves = g.byBase[current]
	}

	return slices.Concat(downstacks, upstacks), nil
}
