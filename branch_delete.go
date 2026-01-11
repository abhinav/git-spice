package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/delete"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type branchDeleteCmd struct {
	BranchPromptConfig

	Force    bool     `help:"Force deletion of the branch"`
	Branches []string `arg:"" optional:"" help:"Names of the branches to delete" predictor:"branches"`
}

func (*branchDeleteCmd) Help() string {
	return text.Dedent(`
		The deleted branches and their commits are removed from the stack.
		Branches above the deleted branches are first rebased onto
		the next branches available downstack,
		or onto trunk if there are no branches available below.

		Without any arguments,
		a prompt will allow selecting the branch to delete.

		By default, if the branch to be deleted has unmerged changes,
		the deletion will be aborted.
		Use --force to delete the branch regardless of unmerged changes.
	`)
}

func (cmd *branchDeleteCmd) AfterApply(
	ctx context.Context,
	wt *git.Worktree,
	view ui.View,
	store *state.Store,
	branchPrompt *branchPrompter,
) error {
	if len(cmd.Branches) == 0 {
		// If a branch name is not given, prompt for one;
		// assuming we're in interactive mode.
		if !ui.Interactive(view) {
			return fmt.Errorf("cannot proceed without branch name: %w", errNoPrompt)
		}

		currentBranch, err := wt.CurrentBranch(ctx)
		if err != nil {
			currentBranch = ""
		}

		branch, err := branchPrompt.Prompt(ctx, &branchPromptRequest{
			Disabled: func(b git.LocalBranch) bool {
				return b.Name == store.Trunk()
			},
			Default:  currentBranch,
			Worktree: wt.RootDir(),
			Title:    "Select a branch to delete",
		})
		if err != nil {
			return fmt.Errorf("select branch: %w", err)
		}

		cmd.Branches = []string{branch}

	}

	return nil
}

// DeleteHandler implements the busines logic for the `branch delete` command.
type DeleteHandler interface {
	DeleteBranches(context.Context, *delete.Request) error
}

func (cmd *branchDeleteCmd) Run(
	ctx context.Context,
	handler DeleteHandler,
) error {
	return handler.DeleteBranches(ctx, &delete.Request{
		Branches: cmd.Branches,
		Force:    cmd.Force,
	})
}
