package main

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/submit"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type upstackSubmitCmd struct {
	submitOptions
	submit.BatchOptions

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
	log *silog.Logger,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
	submitHandler SubmitHandler,
	integrationHandler IntegrationHandler,
) error {
	if cmd.Branch == "" {
		currentBranch, err := wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = currentBranch
	}

	graph, err := svc.BranchGraph(ctx, nil)
	if err != nil {
		return fmt.Errorf("build branch graph: %w", err)
	}

	if cmd.Branch != store.Trunk() {
		if err := cmd.verifyBaseSubmitted(log, graph, store, cmd.Branch); err != nil {
			return err
		}
	}

	upstacks := slices.Collect(graph.Upstack(cmd.Branch))

	// If running from trunk, exclude trunk from the list.
	// Trunk cannot be submitted but everything upstack can.
	if cmd.Branch == store.Trunk() {
		upstacks = upstacks[1:]
	}

	// TODO: separate preparation of the stack from submission

	if err := submitHandler.SubmitBatch(ctx, &submit.BatchRequest{
		Branches:     upstacks,
		Options:      &cmd.Options,
		BatchOptions: &cmd.BatchOptions,
		BranchGraph:  graph,
	}); err != nil {
		return err
	}
	return integrationHandler.MaybeRebuildAndSubmit(ctx)
}

func (cmd *upstackSubmitCmd) verifyBaseSubmitted(
	log *silog.Logger,
	graph *spice.BranchGraph,
	store *state.Store,
	branch string,
) error {
	b, ok := graph.Lookup(branch)
	if !ok {
		return fmt.Errorf("lookup branch %v: %w", branch, git.ErrNotExist)
	}

	if b.Base == store.Trunk() {
		// If base is trunk, this check doesn't apply.
		return nil
	}

	base, ok := graph.Lookup(b.Base)
	if !ok {
		return fmt.Errorf("lookup base %v: %w", b.Base, git.ErrNotExist)
	}

	if base.Change == nil && cmd.Publish {
		log.Errorf("%v: base (%v) has not been submitted", branch, b.Base)
		return errors.New("submit the base branch first")
	}
	return nil
}
