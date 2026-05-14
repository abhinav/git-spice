package main

import (
	"context"

	"go.abhg.dev/gs/internal/handler/submodule"
)

// SubmoduleTracker records submodule branch associations
// for the current worktree state.
type SubmoduleTracker interface {
	RecordBranchState(ctx context.Context, branch string) error
	RecordWithInheritance(ctx context.Context, branch, parentBranch string) error
}

var _ SubmoduleTracker = (*submodule.Tracker)(nil)

// SubmoduleApplier switches tracked submodules to the branches
// recorded for a parent branch, transactionally.
type SubmoduleApplier interface {
	ApplyAssociations(ctx context.Context, parentBranch string) error
}

var _ SubmoduleApplier = (*submodule.Applier)(nil)

type branchSubmoduleCmd struct {
	List    branchSubmoduleListCmd    `cmd:"" aliases:"ls" help:"List submodule branch associations"`
	Repoint branchSubmoduleRepointCmd `cmd:"" help:"Change submodule branch association for the current branch"`
}
