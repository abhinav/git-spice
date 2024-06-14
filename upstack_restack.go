package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/text"
)

type upstackRestackCmd struct {
	Name string `arg:"" optional:"" help:"Branch to restack the upstack of" predictor:"trackedBranches"`

	NoBase bool `help:"Do not restack the base branch"`
}

func (*upstackRestackCmd) Help() string {
	return text.Dedent(`
		Restacks the given branch and all branches above it
		on top of the new heads of their base branches.
		If multiple branches use this branch as their base,
		they will all be restacked.

		If a branch name is not provided,
		the current branch will be used.
		Run this command from the trunk branch
		to restack all managed branches.

		By default, the provided branch is also restacked
		on top of its base branch.
		Use the --no-base flag to only restack branches above it,
		and leave the branch itself untouched.
	`)
}

func (cmd *upstackRestackCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, store, svc, err := openRepo(ctx, log, opts)
	if err != nil {
		return err
	}

	if cmd.Name == "" {
		currentBranch, err := repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Name = currentBranch
	}

	upstacks, err := svc.ListUpstack(ctx, cmd.Name)
	if err != nil {
		return fmt.Errorf("get upstack branches: %w", err)
	}
	if cmd.NoBase && len(upstacks) > 1 && upstacks[0] == cmd.Name {
		upstacks = upstacks[1:]
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
					Branch:  cmd.Name,
					Message: fmt.Sprintf("interrupted: restack upstack of %v", cmd.Name),
				})
			case errors.Is(err, spice.ErrAlreadyRestacked):
				// Log the "does not need to be restacked" message
				// only for branches that are not the base branch.
				if upstack != cmd.Name {
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
	if err := repo.Checkout(ctx, cmd.Name); err != nil {
		return fmt.Errorf("checkout branch %v: %w", cmd.Name, err)
	}

	return nil
}
