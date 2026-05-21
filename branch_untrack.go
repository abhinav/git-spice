package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type branchUntrackCmd struct {
	Branch string `arg:"" optional:"" help:"Name of the branch to untrack. Defaults to current." predictor:"branches"`
}

func (*branchUntrackCmd) Help() string {
	return text.Dedent(`
		The current branch is deleted from git-spice's data store
		but not deleted from the repository.
		Branches upstack from it are not affected,
		and will use the next branch downstack as their new base.

		Provide a branch name as an argument to target
		a different branch.
	`)
}

func (cmd *branchUntrackCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	svc *spice.Service,
	integrationHandler IntegrationHandler,
) error {
	if cmd.Branch == "" {
		var err error
		cmd.Branch, err = wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
	}

	if err := svc.ForgetBranch(ctx, cmd.Branch); err != nil {
		if errors.Is(err, state.ErrNotExist) {
			return errors.New("branch not tracked")
		}

		return fmt.Errorf("forget branch: %w", err)
	}

	if err := integrationHandler.OnBranchRemoved(ctx, cmd.Branch); err != nil {
		log.Warnf("prune integration resolution file: %v", err)
	}

	return nil
}
