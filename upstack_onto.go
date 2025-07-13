package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type upstackOntoCmd struct {
	BranchPromptConfig

	Branch string `help:"Branch to start at" placeholder:"NAME" predictor:"trackedBranches"`
	Onto   string `arg:"" optional:"" help:"Destination branch" predictor:"trackedBranches"`
}

func (*upstackOntoCmd) Help() string {
	return text.Dedent(`
		The current branch and its upstack will move onto the new base.

		For example, given the following stack with B checked out,
		'gs upstack onto main' will have the following effect:

			       gs upstack onto main

			    ┌── C                 ┌── C
			  ┌─┴ B ◀               ┌─┴ B ◀
			┌─┴ A                   ├── A
			trunk                   trunk

		Use 'gs branch onto' to leave the branch's upstack alone.

		Use --branch to move a different branch than the current one.

		A prompt will allow selecting the new base.
		Use the spice.branchPrompt.sort configuration option
		to specify the sort order of branches in the prompt.
		Commonly used field names include "refname", "commiterdate", etc.
		By default, branches are sorted by name.
		Provide the new base name as an argument to skip the prompt.
	`)
}

func (cmd *upstackOntoCmd) AfterApply(
	ctx context.Context,
	log *silog.Logger,
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
		branch, err := svc.LookupBranch(ctx, cmd.Branch)
		if err != nil {
			if errors.Is(err, state.ErrNotExist) {
				return fmt.Errorf("branch not tracked: %s", cmd.Branch)
			}
			return fmt.Errorf("get branch: %w", err)
		}

		if !ui.Interactive(view) {
			return fmt.Errorf("cannot proceed without a destination branch: %w", errNoPrompt)
		}

		upstacks, err := svc.ListUpstack(ctx, cmd.Branch)
		if err != nil {
			log.Warn("Error listing upstack branches", "branch", cmd.Branch, "error", err)
			upstacks = []string{cmd.Branch}
		}
		upstackSet := make(map[string]struct{}, len(upstacks))
		for _, b := range upstacks {
			upstackSet[b] = struct{}{}
		}

		cmd.Onto, err = branchPrompt.Prompt(ctx, &branchPromptRequest{
			Disabled: func(b git.LocalBranch) bool {
				_, isUpstack := upstackSet[b.Name]
				return isUpstack
			},
			TrackedOnly: true,
			Default:     branch.Base,
			Title:       "Select a branch to move onto",
			Description: fmt.Sprintf("Moving the upstack of %s onto another branch", cmd.Branch),
		})
		if err != nil {
			return fmt.Errorf("select branch: %w", err)
		}
	}

	return nil
}

func (cmd *upstackOntoCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	svc *spice.Service,
	restackHandler RestackHandler,
) error {
	// Implementation note:
	// This is a pretty straightforward operation despite the large scope.
	// It starts by rebasing only the current branch onto the target
	// branch, updating internal state to point to the new base.
	// Following that, an 'upstack restack' will handle the upstack branches.
	err := svc.BranchOnto(ctx, &spice.BranchOntoRequest{
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
	log.Infof("%v: moved upstack onto %v", cmd.Branch, cmd.Onto)

	return restackHandler.RestackUpstack(ctx, cmd.Branch, &restack.UpstackOptions{
		SkipStart: true,
	})
}
