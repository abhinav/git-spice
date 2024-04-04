package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
)

type branchEditCmd struct{}

func (*branchEditCmd) Run(ctx context.Context, log *zerolog.Logger) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	// TODO: prompt for init if not initialized
	store, err := state.OpenStore(ctx, repo, log)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	b, err := store.LookupBranch(ctx, currentBranch)
	if err != nil {
		if errors.Is(err, state.ErrNotExist) {
			return fmt.Errorf("branch not tracked: %s", currentBranch)
		}
		return fmt.Errorf("get branch: %w", err)
	}

	if err := repo.Rebase(ctx, git.RebaseRequest{
		Interactive: true,
		Branch:      currentBranch,
		Upstream:    b.Base.Name,
	}); err != nil {
		return fmt.Errorf("rebase: %w", err)
	}

	// TODO: if, when rebase returns, we're in the middle of a rebase,
	// print a message informing the user that they should run
	// `gs continue` after they've finished the rebase operation.

	// TODO: after the rebase operation, restack upstack branches.
	return nil
}
