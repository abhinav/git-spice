package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type stackSubmitCmd struct {
	submitOptions
}

func (*stackSubmitCmd) Help() string {
	return text.Dedent(`
		Change Requests are created or updated
		for all branches in the current stack.
	`) + "\n" + _submitHelp
}

func (cmd *stackSubmitCmd) Run(
	ctx context.Context,
	secretStash secret.Stash,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	store *state.Store,
	svc *spice.Service,
	forges *forge.Registry,
) error {
	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	stack, err := svc.ListStack(ctx, currentBranch)
	if err != nil {
		return fmt.Errorf("list stack: %w", err)
	}

	// TODO: generalize into a service-level method
	// TODO: separate preparation of the stack from submission

	session := newSubmitSession(repo, store, secretStash, forges, view, log)
	for _, branch := range stack {
		if branch == store.Trunk() {
			continue
		}

		err := (&branchSubmitCmd{
			submitOptions: cmd.submitOptions,
			Branch:        branch,
		}).run(ctx, session, log, view, repo, store, svc)
		if err != nil {
			return fmt.Errorf("submit %v: %w", branch, err)
		}
	}

	if cmd.DryRun {
		return nil
	}

	return updateNavigationComments(
		ctx,
		store,
		svc,
		log,
		cmd.NavigationComment,
		session,
	)
}
