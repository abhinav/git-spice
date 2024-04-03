package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
)

type branchUpCmd struct{}

func (*branchUpCmd) Run(ctx context.Context, log *log.Logger) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	// TODO: prompt for init if not initialized
	store, err := state.OpenStore(ctx, repo)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}

	// TODO: ensure no uncommitted changes

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	currentHash, err := repo.PeelToCommit(ctx, currentBranch)
	if err != nil {
		return fmt.Errorf("peel to commit of %q: %w", currentBranch, err)
	}

	children, err := store.UpstackDirect(ctx, currentBranch)
	if err != nil {
		return fmt.Errorf("list children of %q: %w", currentBranch, err)
	}

	switch len(children) {
	case 0:
		return fmt.Errorf("%v: no branches found upstack", currentBranch)
	case 1:
		childName := children[0]
		child, err := store.GetBranch(ctx, childName)
		if err != nil {
			return fmt.Errorf("get branch %q: %w", children[0], err)
		}

		if child.BaseHash == currentHash {
			// Base hasn't changed, just checkout the child.
			if err := repo.Checkout(ctx, children[0]); err != nil {
				return fmt.Errorf("checkout %q: %w", children[0], err)
			}
			return nil
		}

		// Base has changed. Rebase onto the new base.
		req := git.RebaseRequest{
			Onto:     currentBranch,
			Upstream: child.BaseHash.String(),
			Branch:   childName,
		}
		if err := repo.Rebase(ctx, req); err != nil {
			return fmt.Errorf("rebase onto %q: %w", child.BaseHash, err)
		}

		// If the operation was successful, update information in the store.
		if err := store.UpsertBranch(ctx, state.UpsertBranchRequest{
			Name:     childName,
			Base:     currentBranch,
			BaseHash: currentHash,
			Message:  fmt.Sprintf("restack onto %v", currentBranch),
		}); err != nil {
			return fmt.Errorf("update branch %q: %w", childName, err)
		}

	default:
		// TODO: prompt user for which child to checkout
		return fmt.Errorf("not implemented: multiple children")
	}

	return nil
}
