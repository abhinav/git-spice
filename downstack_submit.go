package main

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/gs"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/text"
	"golang.org/x/oauth2"
)

type downstackSubmitCmd struct {
	DryRun bool `short:"n" help:"Don't actually submit the stack"`
	Fill   bool `help:"Fill in the pull request title and body from the commit messages"`

	Name string `arg:"" optional:"" placeholder:"BRANCH" help:"Branch to start at" predictor:"trackedBranches"`
}

func (*downstackSubmitCmd) Help() string {
	return text.Dedent(`
		Submits Pull Requests for the current branch,
		and for all branches below, down to the trunk branch.
		Branches that already have open Pull Requests will be updated.

		A prompt will allow filling metadata about new Pull Requests.
		Use the --fill flag to use the commit messages as-is
		and submit without a prompt.
	`)
}

func (cmd *downstackSubmitCmd) Run(
	ctx context.Context,
	log *log.Logger,
	opts *globalOptions,
	tokenSource oauth2.TokenSource,
) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	store, err := ensureStore(ctx, repo, log, opts)
	if err != nil {
		return err
	}

	if cmd.Name == "" {
		currentBranch, err := repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Name = currentBranch
	}

	if cmd.Name == store.Trunk() {
		return errors.New("nothing to submit below trunk")
	}

	svc := gs.NewService(repo, store, log)
	downstacks, err := svc.ListDownstack(ctx, cmd.Name)
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
			Name:   downstack,
		}).Run(ctx, log, opts, tokenSource)
		if err != nil {
			return fmt.Errorf("submit %v: %w", downstack, err)
		}
	}

	return nil
}
