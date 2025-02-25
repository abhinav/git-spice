package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type upstackSubmitCmd struct {
	submitOptions

	Branch string `placeholder:"NAME" help:"Branch to start at" predictor:"trackedBranches"`
}

func (*upstackSubmitCmd) Help() string {
	return text.Dedent(`
		Change Requests are created or updated
		for the current branch and all branches upstack from it.
		If the base of the current branch is not trunk,
		it must have already been submitted by a prior command.
		Use --branch to start at a different branch.
	`) + "\n" + _submitHelp
}

func (cmd *upstackSubmitCmd) Run(
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

	if cmd.Branch != store.Trunk() {
		b, err := svc.LookupBranch(ctx, cmd.Branch)
		if err != nil {
			return fmt.Errorf("lookup branch %v: %w", cmd.Branch, err)
		}

		if b.Base != store.Trunk() {
			base, err := svc.LookupBranch(ctx, b.Base)
			if err != nil {
				return fmt.Errorf("lookup base %v: %w", b.Base, err)
			}

			if base.Change == nil && cmd.Publish {
				log.Errorf("%v: base (%v) has not been submitted", cmd.Branch, b.Base)
				return errors.New("submit the base branch first")
			}
		}
	}

	upstacks, err := svc.ListUpstack(ctx, cmd.Branch)
	if err != nil {
		return fmt.Errorf("list upstack: %w", err)
	}

	// If running from trunk, exclude trunk from the list.
	// Trunk cannot be submitted but everything upstack can.
	if cmd.Branch == store.Trunk() {
		upstacks = upstacks[1:]
	}

	// TODO: generalize into a service-level method
	// TODO: separate preparation of the stack from submission
	session := newSubmitSession(repo, store, secretStash, forges, view, log)
	for _, b := range upstacks {
		err := (&branchSubmitCmd{
			submitOptions: cmd.submitOptions,
			Branch:        b,
		}).run(ctx, session, log, view, repo, store, svc)
		if err != nil {
			return fmt.Errorf("submit %v: %w", b, err)
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
