package main

import (
	"context"

	"go.abhg.dev/gs/internal/handler/review"
	"go.abhg.dev/gs/internal/text"
)

type reviewResolveCmd struct {
	ThreadID string `arg:"" help:"Thread ID to resolve."`
	Branch   string `short:"b" placeholder:"BRANCH" predictor:"trackedBranches" help:"Branch containing the thread. Defaults to the current branch."`
}

func (*reviewResolveCmd) Help() string {
	return text.Dedent(`
		Resolves a review thread on the change request
		for the current branch.

		The thread ID is shown in 'gs review list'.
	`)
}

func (cmd *reviewResolveCmd) Run(
	ctx context.Context,
	handler ReviewThreadHandler,
) error {
	return handler.SetThreadResolution(ctx, &review.SetThreadResolutionRequest{
		Branch:   cmd.Branch,
		ThreadID: cmd.ThreadID,
		Resolved: true,
	})
}

type reviewReopenCmd struct {
	ThreadID string `arg:"" help:"Thread ID to reopen."`
	Branch   string `short:"b" placeholder:"BRANCH" predictor:"trackedBranches" help:"Branch containing the thread. Defaults to the current branch."`
}

func (*reviewReopenCmd) Help() string {
	return text.Dedent(`
		Reopens a resolved review thread on the change request
		for the current branch.

		The thread ID is shown in 'gs review list'.
	`)
}

func (cmd *reviewReopenCmd) Run(
	ctx context.Context,
	handler ReviewThreadHandler,
) error {
	return handler.SetThreadResolution(ctx, &review.SetThreadResolutionRequest{
		Branch:   cmd.Branch,
		ThreadID: cmd.ThreadID,
		Resolved: false,
	})
}
