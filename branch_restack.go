package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/text"
)

type branchRestackCmd struct {
	AutoResolve bool   `name:"auto-resolve" negatable:"" config:"restack.autoResolve" help:"Auto-resolve rebase conflicts using the configured resolver script"`
	Branch      string `placeholder:"NAME" help:"Branch to restack" predictor:"trackedBranches"`
}

func (*branchRestackCmd) Help() string {
	return text.Dedent(`
		The current branch will be rebased onto its base,
		ensuring a linear history.
		Use --branch to target a different branch.

		With --auto-resolve (or spice.restack.autoResolve=true),
		conflicts encountered during the rebase are passed to the
		configured resolver script before the operation is
		interrupted. See the restack auto-resolve guide for the
		JSON protocol the script must implement.
	`)
}

func (cmd *branchRestackCmd) AfterApply(ctx context.Context, wt *git.Worktree) error {
	if cmd.Branch == "" {
		currentBranch, err := wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = currentBranch
	}
	return nil
}

func (cmd *branchRestackCmd) Run(
	ctx context.Context,
	handler RestackHandler,
	integrationHandler IntegrationHandler,
) error {
	if err := handler.RestackBranch(ctx, cmd.Branch, &restack.Options{
		AutoResolve: &cmd.AutoResolve,
	}); err != nil {
		return err
	}
	return integrationHandler.MaybeRebuild(ctx)
}
