package main

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type downstackSubmitCmd struct {
	submitOptions

	Branch string `placeholder:"NAME" help:"Branch to start at" predictor:"trackedBranches"`
}

func (*downstackSubmitCmd) Help() string {
	return text.Dedent(`
		Change Requests are created or updated
		for the current branch and all branches below it until trunk.
		Use --branch to start at a different branch.
	`) + "\n" + _submitHelp
}

func (cmd *downstackSubmitCmd) Run(
	ctx context.Context,
	secretStash secret.Stash,
	log *log.Logger,
	view ui.View,
	repo *git.Repository,
	store *state.Store,
	svc *spice.Service,
	forges *forge.Registry,
) error {
	if cmd.Branch == "" {
		currentBranch, err := repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = currentBranch
	}

	if cmd.Branch == store.Trunk() {
		return errors.New("nothing to submit below trunk")
	}

	downstacks, err := svc.ListDownstack(ctx, cmd.Branch)
	if err != nil {
		return fmt.Errorf("list downstack: %w", err)
	}
	must.NotBeEmptyf(downstacks, "downstack cannot be empty")
	slices.Reverse(downstacks)

	// TODO: generalize into a service-level method
	// TODO: separate preparation of the stack from submission

	session := newSubmitSession(repo, store, secretStash, forges, view, log)
	for _, downstack := range downstacks {
		err := (&branchSubmitCmd{
			submitOptions: cmd.submitOptions,
			Branch:        downstack,
		}).run(ctx, session, log, view, repo, store, svc)
		if err != nil {
			return fmt.Errorf("submit %v: %w", downstack, err)
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
