package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type upstackRestackCmd struct {
	Branch    string `help:"Branch to restack the upstack of" placeholder:"NAME" predictor:"trackedBranches"`
	SkipStart bool   `help:"Do not restack the starting branch"`
}

func (*upstackRestackCmd) Help() string {
	return text.Dedent(`
		The current branch and all branches above it
		are rebased on top of their respective bases,
		ensuring a linear history.
		Use --branch to start at a different branch.
		Use --skip-start to skip the starting branch,
		but still rebase all branches above it.

		The target branch defaults to the current branch.
		If run from the trunk branch,
		all managed branches will be restacked.
	`)
}

func (cmd *upstackRestackCmd) Run(ctx context.Context, log *log.Logger, view ui.View) error {
	repo, store, svc, err := openRepo(ctx, log, view)
	if err != nil {
		return err
	}

	if cmd.Branch == "" {
		currentBranch, err := repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = currentBranch
	}

	upstacks, err := svc.ListUpstack(ctx, cmd.Branch)
	if err != nil {
		return fmt.Errorf("get upstack branches: %w", err)
	}
	if cmd.SkipStart && len(upstacks) > 0 && upstacks[0] == cmd.Branch {
		upstacks = upstacks[1:]
		if len(upstacks) == 0 {
			return nil
		}
	}

loop:
	for _, upstack := range upstacks {
		// Trunk never needs to be restacked.
		if upstack == store.Trunk() {
			continue loop
		}

		res, err := svc.Restack(ctx, upstack)
		if err != nil {
			var rebaseErr *git.RebaseInterruptError
			switch {
			case errors.As(err, &rebaseErr):
				// If the rebase is interrupted by a conflict,
				// we'll resume by re-running this command.
				return svc.RebaseRescue(ctx, spice.RebaseRescueRequest{
					Err:     rebaseErr,
					Command: []string{"upstack", "restack"},
					Branch:  cmd.Branch,
					Message: fmt.Sprintf("interrupted: restack upstack of %v", cmd.Branch),
				})
			case errors.Is(err, spice.ErrAlreadyRestacked):
				// Log the "does not need to be restacked" message
				// only for branches that are not the base branch.
				if upstack != cmd.Branch {
					log.Infof("%v: branch does not need to be restacked.", upstack)
				}
				continue loop
			default:
				return fmt.Errorf("restack branch: %w", err)
			}
		}

		log.Infof("%v: restacked on %v", upstack, res.Base)
	}

	// On success, check out the original branch.
	if err := repo.Checkout(ctx, cmd.Branch); err != nil {
		return fmt.Errorf("checkout branch %v: %w", cmd.Branch, err)
	}

	return nil
}
