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

type branchRestackCmd struct {
	Branch string `placeholder:"NAME" help:"Branch to restack" predictor:"trackedBranches"`
}

func (*branchRestackCmd) Help() string {
	return text.Dedent(`
		The current branch will be rebased onto its base,
		ensuring a linear history.
		Use --branch to target a different branch.
	`)
}

func (cmd *branchRestackCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	repo *git.Repository,
	svc *spice.Service,
) error {
	if cmd.Branch == "" {
		currentBranch, err := repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = currentBranch
	}

	res, err := svc.Restack(ctx, cmd.Branch)
	if err != nil {
		var rebaseErr *git.RebaseInterruptError
		switch {
		case errors.As(err, &rebaseErr):
			// If the rebase is interrupted by a conflict,
			// we'll resume by re-running this command.
			return svc.RebaseRescue(ctx, spice.RebaseRescueRequest{
				Err:     rebaseErr,
				Command: []string{"branch", "restack"},
				Branch:  cmd.Branch,
				Message: "interrupted: restack branch " + cmd.Branch,
			})
		case errors.Is(err, state.ErrNotExist):
			log.Errorf("%v: branch not tracked: run 'gs branch track'", cmd.Branch)
			return errors.New("untracked branch")
		case errors.Is(err, spice.ErrAlreadyRestacked):
			log.Infof("%v: branch does not need to be restacked.", cmd.Branch)
			return nil
		}
		return fmt.Errorf("restack branch: %w", err)
	}

	log.Infof("%v: restacked on %v", cmd.Branch, res.Base)
	return nil
}
