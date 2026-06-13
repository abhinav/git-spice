package main

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/integration"
	"go.abhg.dev/gs/internal/handler/submit"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type stackSubmitCmd struct {
	submitOptions
	submit.BatchOptions
}

func (*stackSubmitCmd) Help() string {
	return text.Dedent(`
		Change Requests are created or updated
		for all branches in the current stack.

		When invoked from the configured integration branch, the
		"current stack" is the union of each configured tip's
		downstack (the tip and the branches below it), and the
		integration branch itself is pushed afterward. Branches
		above a tip are deliberately left alone: they are work in
		progress that has not been promoted to a tip yet.
	`) + "\n" + _submitHelp
}

func (cmd *stackSubmitCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
	submitHandler SubmitHandler,
	integrationHandler IntegrationHandler,
) error {
	currentBranch, err := wt.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	graph, err := svc.BranchGraph(ctx, nil)
	if err != nil {
		return fmt.Errorf("build branch graph: %w", err)
	}

	// From the integration branch, "the current stack" is the union of
	// every configured tip's full stack. Dispatch into the integration
	// flow so all the tip stacks get submitted and the integration
	// branch itself is pushed in one shot.
	switch info, infoErr := store.Integration(ctx); {
	case infoErr == nil && info.Name == currentBranch:
		return cmd.runFromIntegration(ctx, log, graph, submitHandler, integrationHandler)
	case infoErr != nil && !errors.Is(infoErr, state.ErrNotExist):
		log.Warn("Could not load integration branch configuration", "error", infoErr)
	}

	stack := slices.Collect(graph.Stack(currentBranch))
	if len(stack) == 0 {
		stack = []string{currentBranch}
	}
	toSubmit := stack[:0]
	for _, branch := range stack {
		if branch == store.Trunk() {
			continue
		}
		toSubmit = append(toSubmit, branch)
	}

	// TODO: separate preparation of the stack from submission

	return submitHandler.SubmitBatch(ctx, &submit.BatchRequest{
		Branches:     toSubmit,
		Options:      &cmd.Options,
		BatchOptions: &cmd.BatchOptions,
		BranchGraph:  graph,
	})
}

// runFromIntegration submits the union of every configured tip's
// downstack (the tip and the branches below it), then pushes the
// integration branch itself. Branches above a tip are deliberately
// omitted: an upstack branch that has not been promoted to a tip is
// in-progress and not part of the integration.
func (cmd *stackSubmitCmd) runFromIntegration(
	ctx context.Context,
	log *silog.Logger,
	graph *spice.BranchGraph,
	submitHandler SubmitHandler,
	integrationHandler IntegrationHandler,
) error {
	status, err := integrationHandler.Show(ctx)
	if err != nil {
		return err
	}

	branches := tipDownstackOrder(graph, status.Tips)
	if len(branches) > 0 {
		log.Infof("Submitting %d branch(es) from integration tip stacks", len(branches))
		if err := submitHandler.SubmitBatch(ctx, &submit.BatchRequest{
			Branches:     branches,
			Options:      &cmd.Options,
			BatchOptions: &cmd.BatchOptions,
			BranchGraph:  graph,
		}); err != nil {
			return err
		}
	}

	err = integrationHandler.Submit(ctx)
	if err == nil {
		log.Info("Integration branch pushed.")
		return nil
	}

	var rejected *integration.PushRejectedError
	if errors.As(err, &rejected) {
		log.Error(formatPushRejected(rejected))
	}
	return err
}
