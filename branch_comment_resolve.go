package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type branchCommentResolveCmd struct {
	ThreadID  string `arg:"" help:"Thread ID to resolve."`
	Unresolve bool   `help:"Unresolve the thread instead of resolving it."`
	Branch    string `short:"b" placeholder:"BRANCH" predictor:"trackedBranches" help:"Branch whose change request contains the thread. Defaults to current branch."`
}

func (*branchCommentResolveCmd) Help() string {
	return text.Dedent(`
		Resolves a review thread on the change request
		for the current branch.

		Use --unresolve to mark the thread as unresolved.

		The thread ID is shown in 'gs branch comment list'.
	`)
}

func (cmd *branchCommentResolveCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	svc *spice.Service,
	forgeRepo forge.Repository,
) error {
	branch := cmd.Branch
	if branch == "" {
		var err error
		branch, err = wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
	}

	// Verify branch has a change request.
	b, err := svc.LookupBranch(ctx, branch)
	if err != nil {
		if errors.Is(err, state.ErrNotExist) {
			return fmt.Errorf(
				"branch not tracked: %s", branch,
			)
		}
		return fmt.Errorf("get branch: %w", err)
	}

	if b.Change == nil {
		return fmt.Errorf(
			"no change request for %s", branch,
		)
	}

	resolver, ok := forgeRepo.(forge.WithThreadResolution)
	if !ok {
		return errors.New(
			"forge does not support thread resolution",
		)
	}

	if cmd.Unresolve {
		if err := resolver.UnresolveThread(
			ctx, cmd.ThreadID,
		); err != nil {
			return fmt.Errorf(
				"unresolve thread: %w", err,
			)
		}
		log.Infof("Unresolved thread %s.", cmd.ThreadID)
	} else {
		if err := resolver.ResolveThread(
			ctx, cmd.ThreadID,
		); err != nil {
			return fmt.Errorf("resolve thread: %w", err)
		}
		log.Infof("Resolved thread %s.", cmd.ThreadID)
	}

	return nil
}
