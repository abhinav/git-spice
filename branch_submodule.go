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
// recorded for a parent branch, transactionally, and coordinates
// submodule-side commits at parent commit time.
type SubmoduleApplier interface {
	ApplyAssociations(ctx context.Context, parentBranch string) error
	PreCommitSubmodules(
		ctx context.Context,
		parentBranch string,
		mode submodule.CommitMode,
		msg submodule.CommitMessageSource,
	) (staged []string, err error)
	PostAmendInteractiveSubmodules(
		ctx context.Context, parentBranch string,
	) (staged []string, err error)
}

var _ SubmoduleApplier = (*submodule.Applier)(nil)

type branchSubmoduleCmd struct {
	List    branchSubmoduleListCmd    `cmd:"" aliases:"ls" help:"List submodule branch associations"`
	Repoint branchSubmoduleRepointCmd `cmd:"" help:"Change submodule branch association for the current branch"`
}
