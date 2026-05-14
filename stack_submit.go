package main

import (
	"context"
	"fmt"

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
	forgeRepo *optionalForgeRepository,
	submitHandler SubmitHandler,
	integrationHandler IntegrationHandler,
) error {
	currentBranch, err := wt.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	if err := cmd.checkDownstack(
		ctx, svc, forgeRepo.Repository, currentBranch,
	); err != nil {
		return err
	}

	stack, err := svc.ListStack(ctx, currentBranch)
	if err != nil {
		return fmt.Errorf("list stack: %w", err)
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
	}); err != nil {
		return err
	}
	return integrationHandler.MaybeRebuildAndSubmit(ctx)
}
