package main

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type downstackEditCmd struct {
	Editor string `help:"Editor to use for editing the downstack. Defaults to Git's default editor."`

	Branch string `placeholder:"NAME" help:"Branch to edit from. Defaults to current branch." predictor:"trackedBranches"`
}

func (*downstackEditCmd) Help() string {
	return text.Dedent(`
		An editor opens with a list of branches in-order,
		starting from the current branch until trunk.
		The current branch is at the top of the list.
		Use --branch to start at a different branch.

		Modifications to the list will be reflected in the stack
		when the editor is closed, and the topmost branch will be checked out.
		If the file is cleared, no changes will be made.
		Branches that are deleted from the list will be ignored.
		Branches that are upstack of the current branch will not be modified.
	`)
}

func (cmd *downstackEditCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	store *state.Store,
	svc *spice.Service,
) error {
	if cmd.Editor == "" {
		cmd.Editor = gitEditor(ctx, repo)
	}

	if cmd.Branch == "" {
		currentBranch, err := repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = currentBranch
	}

	if cmd.Branch == store.Trunk() {
		return errors.New("cannot edit below trunk")
	}

	downstacks, err := svc.ListDownstack(ctx, cmd.Branch)
	if err != nil {
		return fmt.Errorf("list downstack: %w", err)
	}
	must.NotBeEmptyf(downstacks, "downstack cannot be empty")
	must.BeEqualf(downstacks[0], cmd.Branch,
		"downstack must start with the original branch")

	if len(downstacks) == 1 {
		log.Infof("nothing to edit below %s", cmd.Branch)
		return nil
	}

	slices.Reverse(downstacks) // branch closest to trunk first
	res, err := svc.StackEdit(ctx, &spice.StackEditRequest{
		Stack:  downstacks,
		Editor: cmd.Editor,
	})
	if err != nil {
		if errors.Is(err, spice.ErrStackEditAborted) {
			log.Infof("downstack edit aborted")
			return nil
		}

		// TODO: we can probably recover from the rebase operation
		// by saving the branch list somewhere,
		// and allowing it to be provided as input to the command.
		return fmt.Errorf("edit downstack: %w", err)
	}

	return (&branchCheckoutCmd{
		Branch: res.Stack[len(res.Stack)-1],
	}).Run(ctx, log, view, repo, store, svc)
}
