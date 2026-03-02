package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type branchDiffCmd struct {
	Branch string `placeholder:"NAME" help:"Branch to diff" predictor:"trackedBranches"`
}

func (*branchDiffCmd) Help() string {
	return text.Dedent(`
		Shows the diff between a branch
		and its base branch in the stack.
		This is equivalent to running
		'git diff base...branch'
		where base is the branch below this one.
		Use --branch to target a different branch.
	`)
}

func (cmd *branchDiffCmd) AfterApply(
	ctx context.Context,
	wt *git.Worktree,
) error {
	if cmd.Branch == "" {
		var err error
		cmd.Branch, err = wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
	}
	return nil
}

func (cmd *branchDiffCmd) Run(
	ctx context.Context,
	wt *git.Worktree,
	svc *spice.Service,
) error {
	b, err := svc.LookupBranch(ctx, cmd.Branch)
	if err != nil {
		if errors.Is(err, state.ErrNotExist) {
			return fmt.Errorf("branch not tracked: %s", cmd.Branch)
		}
		return fmt.Errorf("get branch: %w", err)
	}

	return wt.DiffBranch(ctx, b.Base, cmd.Branch)
}
