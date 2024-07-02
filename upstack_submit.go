package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/text"
)

type upstackSubmitCmd struct {
	DryRun bool `short:"n" help:"Don't actually submit the stack"`
	Fill   bool `help:"Fill in the pull request title and body from the commit messages"`

	Branch string `placeholder:"NAME" help:"Branch to start at" predictor:"trackedBranches"`
}

func (*upstackSubmitCmd) Help() string {
	return text.Dedent(`
		Change Requests are created or updated
		for the current branch and all branches upstack from it.
		If the base of the current branch is not trunk,
		it must have already been submitted by a prior command.
		Use --branch to start at a different branch.

		A prompt will ask for a title and body for each Change Request.
		Use --fill to populate these from the commit messages.
		Use --dry-run to see what would be submitted
		without actually submitting anything.
	`)
}

func (cmd *upstackSubmitCmd) Run(
	ctx context.Context,
	secretStash secret.Stash,
	log *log.Logger,
	opts *globalOptions,
) error {
	repo, store, svc, err := openRepo(ctx, log, opts)
	if err != nil {
		return err
	}

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

			if base.Change == nil {
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
	// TODO: submits should be done in parallel
	for _, b := range upstacks {
		err := (&branchSubmitCmd{
			DryRun: cmd.DryRun,
			Fill:   cmd.Fill,
			Branch: b,
		}).Run(ctx, secretStash, log, opts)
		if err != nil {
			return fmt.Errorf("submit %v: %w", b, err)
		}
	}

	return nil
}
