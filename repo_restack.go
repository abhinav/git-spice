package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/autostash"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type repoRestackCmd struct {
	SkipConflicts bool `help:"Skip branches that cannot be rebased due to conflicts"`
}

func (*repoRestackCmd) Help() string {
	return text.Dedent(`
		All tracked branches in the repository are rebased on top of their
		respective bases in dependency order, ensuring a linear history.

		Use --skip-conflicts to skip branches that cannot be rebased cleanly.
	`)
}

func (cmd *repoRestackCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	store *state.Store,
	handler RestackHandler,
	autostashHandler AutostashHandler,
) (retErr error) {
	currentBranch, err := wt.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	cleanup, err := autostashHandler.BeginAutostash(ctx, &autostash.Options{
		Message:   "git-spice: autostash before restacking",
		ResetMode: autostash.ResetHard,
		Branch:    currentBranch,
	})
	if err != nil {
		return err
	}
	defer cleanup(&retErr, nil)

	count, err := handler.Restack(ctx, &restack.Request{
		Branch:          store.Trunk(),
		Scope:           restack.ScopeUpstackExclusive,
		ContinueCommand: []string{"repo", "restack"},
		SkipConflicts:   cmd.SkipConflicts,
	})
	if err != nil {
		return err
	}

	if count == 0 {
		log.Infof("Nothing to restack: no tracked branches available")
		return nil
	}

	if err := wt.CheckoutBranch(ctx, currentBranch); err != nil {
		return fmt.Errorf("checkout %v: %w", currentBranch, err)
	}

	log.Infof("Restacked %d branches", count)
	return nil
}
