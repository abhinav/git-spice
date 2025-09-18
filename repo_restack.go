package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type repoRestackCmd struct{}

func (*repoRestackCmd) Help() string {
	return text.Dedent(`
		All tracked branches in the repository are rebased on top of their
		respective bases in dependency order, ensuring a linear history.
	`)
}

func (*repoRestackCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	store *state.Store,
	service *spice.Service,
	handler RestackHandler,
) (retErr error) {
	currentBranch, err := wt.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	stashMsg := "git-spice: autostash before restacking"
	if stashHash, err := wt.StashCreate(ctx, stashMsg); err != nil {
		if !errors.Is(err, git.ErrNoChanges) {
			return fmt.Errorf("stash changes: %w", err)
		}
		// No changes to stash, that's fine.
	} else {
		// We created a stash.
		// We will reset the working tree to HEAD
		// (losing any uncommitted changes),
		// then one of the following:
		//
		//  - if the command exits with success,
		//    we will pop the stash to restore the changes.
		//  - if the command exits with an error,
		//    schedule an "internal autostash-pop" command
		//    to be run when the rebase operation is finished.
		if err := wt.Reset(ctx, "HEAD", git.ResetOptions{Mode: git.ResetHard}); err != nil {
			return fmt.Errorf("reset before restack: %w", err)
		}

		defer func() {
			if retErr == nil {
				retErr = (&internalAutostashPop{
					Hash: stashHash.String(),
				}).Run(ctx, log, wt)
				return
			}

			retErr = service.RebaseRescue(ctx, spice.RebaseRescueRequest{
				Err:     retErr,
				Command: []string{"internal", "autostash-pop", stashHash.String()},
				Branch:  currentBranch,
				Message: fmt.Sprintf("interrupted: restore stashed changes %q", stashHash),
			})
		}()
	}

	count, err := handler.Restack(ctx, &restack.Request{
		Branch:          store.Trunk(),
		Scope:           restack.ScopeUpstackExclusive,
		ContinueCommand: []string{"repo", "restack"},
	})
	if err != nil {
		return err
	}

	if count == 0 {
		log.Infof("Nothing to restack: no tracked branches available")
		return nil
	}

	if err := wt.Checkout(ctx, currentBranch); err != nil {
		return fmt.Errorf("checkout %v: %w", currentBranch, err)
	}

	log.Infof("Restacked %d branches", count)
	return nil
}
