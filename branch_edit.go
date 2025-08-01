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

type branchEditCmd struct{}

func (*branchEditCmd) Help() string {
	return text.Dedent(`
		Starts an interactive rebase with only the commits
		in this branch.
		Following the rebase, branches upstack from this branch
		will be restacked.
	`)
}

func (*branchEditCmd) Run(
	ctx context.Context,
	wt *git.Worktree,
	svc *spice.Service,
	restackHandler RestackHandler,
) error {
	currentBranch, err := wt.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	b, err := svc.LookupBranch(ctx, currentBranch)
	if err != nil {
		if errors.Is(err, state.ErrNotExist) {
			return fmt.Errorf("branch not tracked: %s", currentBranch)
		}
		return fmt.Errorf("get branch: %w", err)
	}

	req := git.RebaseRequest{
		Interactive: true,
		Branch:      currentBranch,
		Upstream:    b.Base,
	}
	if err := wt.Rebase(ctx, req); err != nil {
		// if the rebase is interrupted,
		// recover with an 'upstack restack' later.
		return svc.RebaseRescue(ctx, spice.RebaseRescueRequest{
			Err:     err,
			Command: []string{"upstack", "restack"},
			Branch:  currentBranch,
			Message: "interrupted: edit branch " + currentBranch,
		})
	}

	return restackHandler.RestackUpstack(ctx, currentBranch, nil)
}
