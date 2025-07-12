package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type stackEditCmd struct {
	Editor string `help:"Editor to use for editing the downstack. Defaults to Git's default editor."`

	Branch string `placeholder:"NAME" help:"Branch whose stack we're editing. Defaults to current branch." predictor:"trackedBranches"`
}

func (*stackEditCmd) Help() string {
	return text.Dedent(`
		This operation requires a linear stack:
		no branch can have multiple branches above it.

		An editor opens with a list of branches in the current stack in-order,
		with the topmost branch at the top of the file,
		and the branch closest to the trunk at the bottom.

		Modifications to the list will be reflected in the stack
		when the editor is closed.
		If the file is cleared, no changes will be made.
		Branches that are deleted from the list will be ignored.
	`)
}

func (cmd *stackEditCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
	trackHandler TrackHandler,
) error {
	if cmd.Editor == "" {
		cmd.Editor = gitEditor(ctx, repo)
	}

	if cmd.Branch == "" {
		currentBranch, err := wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = currentBranch
	}

	stack, err := svc.ListStackLinear(ctx, cmd.Branch)
	if err != nil {
		var nonLinearErr *spice.NonLinearStackError
		if errors.As(err, &nonLinearErr) {
			// TODO: We could provide a prompt here to select a linear stack to edit from.
			log.Errorf("%v is part of a stack with a divergent upstack.", cmd.Branch)
			log.Errorf("%v has multiple branches above it: %s", nonLinearErr.Branch, strings.Join(nonLinearErr.Aboves, ", "))
			log.Errorf("Check out one of those branches and try again.")
			return errors.New("current branch has ambiguous upstack")
		}
		return fmt.Errorf("list stack: %w", err)
	}

	// If current branch was trunk, it'll be at the bottom of the stack.
	if stack[0] == store.Trunk() {
		stack = stack[1:]
	}

	if len(stack) == 1 {
		log.Info("nothing to edit")
		return nil
	}

	_, err = svc.StackEdit(ctx, &spice.StackEditRequest{
		Editor: cmd.Editor,
		Stack:  stack,
	})
	if err != nil {
		if errors.Is(err, spice.ErrStackEditAborted) {
			log.Infof("stack edit aborted")
			return nil
		}

		// TODO: we can probably recover from the rebase operation
		// by saving the branch list somewhere,
		// and allowing it to be provided as input to the command.
		return fmt.Errorf("edit downstack: %w", err)
	}

	return (&branchCheckoutCmd{
		Branch: cmd.Branch,
	}).Run(ctx, log, view, wt, store, svc, trackHandler)
}
