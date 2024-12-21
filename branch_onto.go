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
	"go.abhg.dev/gs/internal/ui"
)

type branchOntoCmd struct {
	Branch string `help:"Branch to move" placeholder:"NAME" predictor:"trackedBranches"`
	Onto   string `arg:"" optional:"" help:"Destination branch" predictor:"trackedBranches"`
}

func (*branchOntoCmd) Help() string {
	return text.Dedent(`
		The commits of the current branch are transplanted onto another
		branch.
		Branches upstack are moved to point to its original base.
		Use --branch to move a different branch than the current one.

		A prompt will allow selecting the new base.
		Provide the new base name as an argument to skip the prompt.

		For example, given the following stack with B checked out,
		running 'gs branch onto main' will move B onto main
		and leave C on top of A.

			       gs branch onto main

			    ┌── C               ┌── B ◀
			  ┌─┴ B ◀               │ ┌── C
			┌─┴ A                   ├─┴ A
			trunk                   trunk
	`)
}

func (cmd *branchOntoCmd) Run(
	ctx context.Context,
	log *log.Logger,
	view ui.View,
	repo *git.Repository,
	store *state.Store,
	svc *spice.Service,
) error {
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
		if !ui.Interactive(view) {
			return fmt.Errorf("cannot proceed without a destination branch: %w", errNoPrompt)
		}

		cmd.Onto, err = (&branchPrompt{
			Disabled: func(b git.LocalBranch) bool {
				return b.Name == cmd.Branch
			},
			TrackedOnly: true,
			Default:     branch.Base,
			Title:       "Select a branch to move onto",
			Description: fmt.Sprintf("Moving %s onto another branch", cmd.Branch),
		}).Run(ctx, view, repo, store)
		if err != nil {
			return fmt.Errorf("select branch: %w", err)
		}
	}

	aboves, err := svc.ListAbove(ctx, cmd.Branch)
	if err != nil {
		return fmt.Errorf("list branches above %s: %w", cmd.Branch, err)
	}

	// As long as there are any branches above this one,
	// they need to be grafted onto this branch's original base.
	// However, this move operation will be an 'upstack onto'
	// as for each of these branches, we want to keep *their* upstacks.
	for _, above := range aboves {
		if err := (&upstackOntoCmd{
			Branch: above,
			Onto:   branch.Base,
		}).Run(ctx, log, view, repo, store, svc); err != nil {
			return svc.RebaseRescue(ctx, spice.RebaseRescueRequest{
				Err:     err,
				Command: []string{"branch", "onto", cmd.Onto},
				Branch:  cmd.Branch,
				Message: fmt.Sprintf("interrupted: %s: branch onto %s", cmd.Branch, cmd.Onto),
			})
		}
	}

	// Only after the upstacks have been moved
	// will we move the branch itself and update its internal state.
	if err := svc.BranchOnto(ctx, &spice.BranchOntoRequest{
		Branch: cmd.Branch,
		Onto:   cmd.Onto,
	}); err != nil {
		// If the rebase is interrupted,
		// we'll just re-run this command again later.
		return svc.RebaseRescue(ctx, spice.RebaseRescueRequest{
			Err:     err,
			Command: []string{"branch", "onto", cmd.Onto},
			Branch:  cmd.Branch,
			Message: fmt.Sprintf("interrupted: %s: branch onto %s", cmd.Branch, cmd.Onto),
		})
	}

	log.Infof("%s: moved onto %s", cmd.Branch, cmd.Onto)
	return repo.Checkout(ctx, cmd.Branch)
}
