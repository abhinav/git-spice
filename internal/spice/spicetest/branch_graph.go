// Package spicetest provides test fixtures for spice package concepts.
package spicetest

import (
	"context"
	"slices"

	"github.com/stretchr/testify/require"

	"go.abhg.dev/gs/internal/spice"
)

// T is the subset of test APIs needed to build fixtures.
type T interface {
	Context() context.Context
	Helper()
	Errorf(string, ...any)
	FailNow()
}

// BranchGraphConfig specifies the branch graph fixture
// that a test wants to build.
//
// It keeps test setup close to the graph's domain model:
// Branches are the tracked branch records,
// Trunk is the repository trunk,
// and Worktrees records optional branch checkout locations.
type BranchGraphConfig struct {
	// Trunk is the repository trunk branch.
	Trunk string

	// Branches are the tracked branches in the graph.
	Branches []spice.LoadBranchItem

	// Worktrees maps branch names to checkout directories.
	//
	// When this map is non-empty,
	// NewBranchGraph asks the graph builder to load worktree information.
	Worktrees map[string]string
}

// NewBranchGraph builds a branch graph fixture for tests.
func NewBranchGraph(t T, cfg BranchGraphConfig) *spice.BranchGraph {
	t.Helper()

	graph, err := spice.NewBranchGraph(
		t.Context(),
		&branchLoader{
			trunk:     cfg.Trunk,
			branches:  cfg.Branches,
			worktrees: cfg.Worktrees,
		},
		&spice.BranchGraphOptions{
			IncludeWorktrees: len(cfg.Worktrees) > 0,
		},
	)
	require.NoError(t, err)
	return graph
}

type branchLoader struct {
	trunk     string
	branches  []spice.LoadBranchItem
	worktrees map[string]string
}

var _ spice.BranchLoader = (*branchLoader)(nil)

func (l *branchLoader) Trunk() string {
	return l.trunk
}

func (l *branchLoader) LoadBranches(
	context.Context,
) ([]spice.LoadBranchItem, error) {
	return slices.Clone(l.branches), nil
}

func (l *branchLoader) LookupWorktrees(
	_ context.Context,
	branches []string,
) (map[string]string, error) {
	worktrees := make(map[string]string)
	for _, branch := range branches {
		worktree := l.worktrees[branch]
		if worktree != "" {
			worktrees[branch] = worktree
		}
	}
	return worktrees, nil
}
