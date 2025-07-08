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

type upstackDeleteCmd struct {
	Force bool `help:"Force deletion of the branches"`
}

func (*upstackDeleteCmd) Help() string {
	return text.Dedent(`
		Deletes all branches above the current branch in the stack,
		not including the current branch.
		The current branch remains unchanged.

		This is a convenient way to clean up abandoned or completed
		parts of a feature stack.

		As this is a destructive operation,
		you must use the --force flag to confirm deletion.
	`)
}

func (cmd *upstackDeleteCmd) Run(
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

	upstack, err := svc.ListUpstack(ctx, currentBranch)
	if err != nil {
		return fmt.Errorf("list upstack: %w", err)
	}
	// upstack[0] is always the current branch, so skip it.
	upstack = upstack[1:]
	if len(upstack) == 0 {
		log.Infof("%v: no upstack branches to delete", currentBranch)
		return nil
	}

	prefix := "WOULD "
	shouldPrompt := !cmd.Force && ui.Interactive(view)
	if shouldPrompt {
		prefix = "WILL "
	}
	for _, branch := range upstack {
		log.Infof("%s delete branch: %v", prefix, branch)
	}

	if shouldPrompt {
		prompt := ui.NewConfirm().
			WithTitlef("Delete %d upstack branches", len(upstack)).
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
		Branches: upstack,
		Force:    true,
	}).Run(ctx, log, view, repo, wt, store, svc)
}
