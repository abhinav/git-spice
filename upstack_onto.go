package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type upstackOntoCmd struct {
	Branch string `help:"Branch to start at" placeholder:"NAME" predictor:"trackedBranches"`
	Onto   string `arg:"" optional:"" help:"Destination branch" predictor:"trackedBranches"`
}

func (*upstackOntoCmd) Help() string {
	return text.Dedent(`
		Moves a branch and its upstack branches onto another branch.
		Use this to move a complete part of your branch stack to a
		different base.

		For example, given the following stack with B checked out,
		running 'gs upstack onto main' will move B and C onto main:

			       gs upstack onto main

			    ┌── C                 ┌── C
			  ┌─┴ B ◀               ┌─┴ B ◀
			┌─┴ A                   ├── A
			trunk                   trunk
	`)
}

func (cmd *upstackOntoCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, store, svc, err := openRepo(ctx, log, opts)
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
	if cmd.Branch == store.Trunk() {
		return fmt.Errorf("cannot move trunk")
	}

	branch, err := svc.LookupBranch(ctx, cmd.Branch)
	if err != nil {
		if errors.Is(err, state.ErrNotExist) {
			return fmt.Errorf("branch not tracked: %s", cmd.Branch)
		}
		return fmt.Errorf("get branch: %w", err)
	}

	if cmd.Onto == "" {
		if !opts.Prompt {
			return fmt.Errorf("cannot proceed without a destination branch: %w", errNoPrompt)
		}

		cmd.Onto, err = (&branchPrompt{
			Exclude:     []string{cmd.Branch},
			TrackedOnly: true,
			Default:     branch.Base,
			Title:       "Select a branch to move onto",
			Description: fmt.Sprintf("Moving the upstack of %s onto another branch", cmd.Branch),
		}).Run(ctx, repo, store)
		if err != nil {
			return fmt.Errorf("select branch: %w", err)
		}
	}

	if branch.Base == cmd.Onto {
		log.Infof("%s: already on %s", cmd.Branch, cmd.Onto)
		return nil
	}

	// Implementation note:
	// This is a pretty straightforward operation despite the large scope.
	// It starts by rebasing only the current branch onto the target
	// branch, updating internal state to point to the new base.
	// Following that, an 'upstack restack' will handle the upstack branches.
	err = svc.BranchOnto(ctx, &spice.BranchOntoRequest{
		Branch: cmd.Branch,
		Onto:   cmd.Onto,
	})
	if err != nil {
		// If the rebase is interrupted,
		// we'll just re-run this command again later.
		return svc.RebaseRescue(ctx, spice.RebaseRescueRequest{
			Err:     err,
			Command: []string{"upstack", "onto", cmd.Onto},
			Branch:  cmd.Branch,
			Message: fmt.Sprintf("interrupted: %s: upstack onto %s", cmd.Branch, cmd.Onto),
		})
	}

	return (&upstackRestackCmd{
		NoBase: true, // we've already moved the current branch
	}).Run(ctx, log, opts)
}
