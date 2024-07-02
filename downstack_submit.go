package main

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/text"
)

type downstackSubmitCmd struct {
	DryRun bool `short:"n" help:"Don't actually submit the stack"`
	Fill   bool `help:"Fill in the pull request title and body from the commit messages"`

	Branch string `placeholder:"NAME" help:"Branch to start at" predictor:"trackedBranches"`
}

func (*downstackSubmitCmd) Help() string {
	return text.Dedent(`
		Change Requests are created or updated
		for the current branch and all branches below it until trunk.
		Use --branch to start at a different branch.

		A prompt will ask for a title and body for each Change Request.
		Use --fill to populate these from the commit messages.
		Use --dry-run to see what would be submitted
		without actually submitting anything.
	`)
}

func (cmd *downstackSubmitCmd) Run(
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
	// TODO: submits should be done in parallel
	for _, downstack := range downstacks {
		err := (&branchSubmitCmd{
			DryRun: cmd.DryRun,
			Fill:   cmd.Fill,
			Branch: downstack,
		}).Run(ctx, secretStash, log, opts)
		if err != nil {
			return fmt.Errorf("submit %v: %w", downstack, err)
		}
	}

	return nil
}
