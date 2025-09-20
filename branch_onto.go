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
	"go.abhg.dev/gs/internal/ui"
)

type branchOntoCmd struct {
	BranchPromptConfig

	Branch string `help:"Branch to move" placeholder:"NAME" predictor:"trackedBranches"`
	Onto   string `arg:"" optional:"" help:"Destination branch" predictor:"trackedBranches"`
}

func (*branchOntoCmd) Help() string {
	return text.Dedent(`
		The commits of the current branch are transplanted onto another
		branch.
		Branches upstack are moved to point to its original base.

		For example, given the following stack with B checked out,
		running 'gs branch onto main' will move B onto main
		and leave C on top of A.

			       gs branch onto main

			    ┌── C               ┌── B ◀
			  ┌─┴ B ◀               │ ┌── C
			┌─┴ A                   ├─┴ A
			trunk                   trunk

		Use --branch to move a different branch than the current one.

		A prompt will allow selecting the new base.
		Use the spice.branchPrompt.sort configuration option
		to specify the sort order of branches in the prompt.
		Commonly used field names include "refname", "commiterdate", etc.
		By default, branches are sorted by name.
		Provide the new base name as an argument to skip the prompt.
	`)
}

func (cmd *branchOntoCmd) AfterApply(
	ctx context.Context,
	view ui.View,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
	branchPrompt *branchPrompter,
) error {
	if cmd.Branch == "" {
		currentBranch, err := wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = currentBranch
	}

	if cmd.Branch == store.Trunk() {
		return errors.New("cannot move trunk")
	}

	if cmd.Onto == "" {
		if !ui.Interactive(view) {
			return fmt.Errorf("cannot proceed without a destination branch: %w", errNoPrompt)
		}

		// TODO: cache between AfterApply and Run?
		branch, err := svc.LookupBranch(ctx, cmd.Branch)
		if err != nil {
			if errors.Is(err, state.ErrNotExist) {
				return fmt.Errorf("branch not tracked: %s", cmd.Branch)
			}
			return fmt.Errorf("get branch: %w", err)
		}

		cmd.Onto, err = branchPrompt.Prompt(ctx, &branchPromptRequest{
			Disabled: func(b git.LocalBranch) bool {
				return b.Name == cmd.Branch
			},
			TrackedOnly: true,
			Default:     branch.Base,
			Title:       "Select a branch to move onto",
			Description: fmt.Sprintf("Moving %s onto another branch", cmd.Branch),
		})
		if err != nil {
			return fmt.Errorf("select branch: %w", err)
		}
	}

	return nil
}

func (cmd *branchOntoCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	svc *spice.Service,
	restackHandler RestackHandler,
) error {
	branch, err := svc.LookupBranch(ctx, cmd.Branch)
	if err != nil {
		if errors.Is(err, state.ErrNotExist) {
			return fmt.Errorf("branch not tracked: %s", cmd.Branch)
		}
		return fmt.Errorf("get branch: %w", err)
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
		}).Run(ctx, log, svc, restackHandler); err != nil {
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
	return wt.CheckoutBranch(ctx, cmd.Branch)
}
