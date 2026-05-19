package main

import (
	"context"
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/submit"
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
	`) + "\n" + _submitHelp
}

func (cmd *stackSubmitCmd) Run(
	ctx context.Context,
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

	if err := submitHandler.SubmitBatch(ctx, &submit.BatchRequest{
		Branches:     toSubmit,
		Options:      &cmd.Options,
		BatchOptions: &cmd.BatchOptions,
		BranchGraph:  graph,
	}); err != nil {
		return err
	}
	return integrationHandler.MaybeRebuildAndSubmit(ctx)
}
