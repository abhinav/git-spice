package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/text"
)

type stackSubmitCmd struct {
	DryRun bool `short:"n" help:"Don't actually submit the stack"`
	Fill   bool `help:"Fill in the pull request title and body from the commit messages"`
}

func (*stackSubmitCmd) Help() string {
	return text.Dedent(`
		Change Requests are created or updated
		for all branches in the current stack.
		A prompt will ask for a title and body for each Change Request.
		Use --fill to populate these from the commit messages.
		Use --dry-run to see what would be submitted
		without actually submitting anything.
	`)
}

func (cmd *stackSubmitCmd) Run(
	ctx context.Context,
	secretStash secret.Stash,
	log *log.Logger,
	opts *globalOptions,
) error {
	repo, store, svc, err := openRepo(ctx, log, opts)
	if err != nil {
		return err
	}

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
	// TODO: submits should be done in parallel
	for _, branch := range stack {
		if branch == store.Trunk() {
			continue
		}

		err := (&branchSubmitCmd{
			DryRun: cmd.DryRun,
			Fill:   cmd.Fill,
			Branch: branch,
		}).Run(ctx, secretStash, log, opts)
		if err != nil {
			return fmt.Errorf("submit %v: %w", branch, err)
		}
	}

	return nil
}
