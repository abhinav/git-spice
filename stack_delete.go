package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type stackDeleteCmd struct {
	Force bool `help:"Force deletion of the branches"`
}

func (*stackDeleteCmd) Help() string {
	return text.Dedent(`
		Deletes all branches in the current branch's stack.
		This includes both upstack and downstack branches.

		The deleted branches and their commits are removed from the stack.
		This is a convenient way to clean up completed or abandoned
		feature stacks.

		As this is a destructive operation,
		you must use the --force flag to confirm deletion.
	`)
}

func (cmd *stackDeleteCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
) error {
	currentBranch, err := wt.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	if currentBranch == store.Trunk() {
		return errors.New("this command cannot be run against the trunk branch")
	}

	stack, err := svc.ListStack(ctx, currentBranch)
	if err != nil {
		return fmt.Errorf("list stack: %w", err)
	}
	must.NotBeEmptyf(stack, "list stack from non-trunk branch cannot be empty: %s", currentBranch)

	prefix := "WOULD "
	shouldPrompt := !cmd.Force && ui.Interactive(view)
	if shouldPrompt {
		prefix = "WILL "
	}
	for _, branch := range stack {
		log.Infof("%s delete branch: %v", prefix, branch)
	}

	if shouldPrompt {
		prompt := ui.NewConfirm().
			WithTitlef("Delete %d branches", len(stack)).
			WithDescription("Confirm all these branches should be deleted.").
			WithValue(&cmd.Force)
		if err := ui.Run(view, prompt); err != nil {
			return err
		}
	}

	if !cmd.Force {
		return errors.New("use --force to confirm deletion")
	}

	return (&branchDeleteCmd{
		Branches: stack,
		Force:    true,
	}).Run(ctx, log, view, repo, wt, store, svc)
}
