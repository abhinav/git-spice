package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/squash"
	"go.abhg.dev/gs/internal/text"
)

type branchSquashCmd struct {
	squash.Options

	Branch string `released:"unreleased" help:"Branch to squash. Defaults to current branch." predictor:"trackedBranches" placeholder:"NAME"`
}

func (*branchSquashCmd) Help() string {
	return text.Dedent(`
		Squash all commits in the current branch into a single commit
		and restack upstack branches.

		An editor will open to edit the commit message of the squashed commit.
		Use the -m/--message flag to specify a commit message without editing.
	`)
}

// SquashHandler is a subset of squash.Handler.
type SquashHandler interface {
	SquashBranch(ctx context.Context, branchName string, opts *squash.Options) error
}

var _ SquashHandler = (*squash.Handler)(nil)

func (cmd *branchSquashCmd) AfterApply(ctx context.Context, wt *git.Worktree) error {
	if cmd.Branch == "" {
		branch, err := wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = branch
	}

	return nil
}

func (cmd *branchSquashCmd) Run(ctx context.Context, squashHandler SquashHandler) (err error) {
	return squashHandler.SquashBranch(ctx, cmd.Branch, &cmd.Options)
}
