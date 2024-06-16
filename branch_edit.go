package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type branchEditCmd struct{}

func (*branchEditCmd) Help() string {
	return text.Dedent(`
		Begins an interactive rebase of a branch without affecting its
		base branch. This allows you to edit the commits in the branch,
		reword their messages, etc.
		After the rebase, the branches upstack from the edited branch
		will be restacked.
	`)
}

func (*branchEditCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, _, svc, err := openRepo(ctx, log, opts)
	if err != nil {
		return err
	}

	currentBranch, err := repo.CurrentBranch(ctx)
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
	if err := repo.Rebase(ctx, req); err != nil {
		// if the rebase is interrupted,
		// recover with an 'upstack restack' later.
		return svc.RebaseRescue(ctx, spice.RebaseRescueRequest{
			Err:     err,
			Command: []string{"upstack", "restack"},
			Branch:  currentBranch,
			Message: fmt.Sprintf("interrupted: edit branch %s", currentBranch),
		})
	}

	return (&upstackRestackCmd{}).Run(ctx, log, opts)
}
