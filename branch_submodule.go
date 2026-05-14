package main

import (
	"context"

	"go.abhg.dev/gs/internal/handler/submodule"
)

// SubmoduleTracker records submodule branch associations
// for the current worktree state.
type SubmoduleTracker interface {
	RecordBranchState(ctx context.Context, branch string) error
}

var _ SubmoduleTracker = (*submodule.Tracker)(nil)

type branchSubmoduleCmd struct {
	List    branchSubmoduleListCmd    `cmd:"" aliases:"ls" help:"List submodule branch associations"`
	Repoint branchSubmoduleRepointCmd `cmd:"" help:"Change submodule branch association for the current branch"`
}
