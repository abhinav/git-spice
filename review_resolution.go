package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
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
	log *silog.Logger,
	wt *git.Worktree,
	svc *spice.Service,
	forgeRepo forge.Repository,
) error {
	return setReviewThreadResolved(
		ctx,
		log,
		wt,
		svc,
		forgeRepo,
		cmd.Branch,
		cmd.ThreadID,
		true,
	)
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
	log *silog.Logger,
	wt *git.Worktree,
	svc *spice.Service,
	forgeRepo forge.Repository,
) error {
	return setReviewThreadResolved(
		ctx,
		log,
		wt,
		svc,
		forgeRepo,
		cmd.Branch,
		cmd.ThreadID,
		false,
	)
}

func setReviewThreadResolved(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	svc *spice.Service,
	forgeRepo forge.Repository,
	branch string,
	thread string,
	resolved bool,
) error {
	branch, err := reviewBranch(ctx, wt, branch)
	if err != nil {
		return err
	}
	b, reviewRepo, err := reviewRepositoryForBranch(
		ctx, svc, forgeRepo, branch,
	)
	if err != nil {
		return err
	}
	resolver, ok := forgeRepo.(forge.ReviewThreadResolver)
	if !ok {
		return errors.New(
			"forge does not support review thread resolution",
		)
	}

	threadIDs, err := loadReviewThreadIDs(
		ctx,
		reviewRepo,
		b.Change.ChangeID(),
	)
	if err != nil {
		return err
	}
	threadID, err := reviewThreadID(threadIDs, thread)
	if err != nil {
		return err
	}

	if resolved {
		if err := resolver.ResolveReviewThread(ctx, threadID); err != nil {
			return fmt.Errorf("resolve thread: %w", err)
		}
		log.Infof("Resolved thread %s.", thread)
		return nil
	}
	if err := resolver.UnresolveReviewThread(ctx, threadID); err != nil {
		return fmt.Errorf("reopen thread: %w", err)
	}
	log.Infof("Reopened thread %s.", thread)
	return nil
}
