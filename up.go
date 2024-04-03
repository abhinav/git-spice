package main

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
)

type upCmd struct{}

func (*upCmd) Run(ctx context.Context, log *zerolog.Logger) error {
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

	var targetName string
	switch len(children) {
	case 0:
		return fmt.Errorf("%v: no branches found upstack", currentBranch)
	case 1:
		targetName = children[0]
	default:
		// TODO: prompt user for which child to checkout
		return fmt.Errorf("not implemented: multiple children")
	}

	targetBranch, err := store.GetBranch(ctx, targetName)
	if err != nil {
		return fmt.Errorf("get branch %q: %w", children[0], err)
	}

	if targetBranch.BaseHash != currentHash {
		var resolved bool

		// Case 1:
		// The user has already fixed the branch.
		// Our information is stale, and we just need to update that.
		mergeBase, err := repo.MergeBase(ctx, currentBranch, targetName)
		if err == nil && mergeBase == currentHash {
			resolved = true
			targetBranch.BaseHash = currentHash
			_ = store.UpsertBranch(ctx, state.UpsertBranchRequest{
				Name:     targetName,
				BaseHash: mergeBase,
				Message:  fmt.Sprintf("%s: rebased externally on %s", targetName, currentBranch),
			})
			// TODO: error handling
		}

		// Case 2:
		// Current branch has diverged from what the target branch
		// was forked from. That is:
		//
		//  ---X---A'---o current
		//      \
		//       A
		//        \
		//         B---o---o target
		//
		// Upstack was forked from our branch when the child of X was A.
		// Since then, we have amended A to get A',
		// but the target branch still points to A.
		//
		// In this case, merge-base --fork-point will give us A,
		// and that should be the base of the target branch.
		//
		// The user needs to run restack to fix this,
		// but we can at least update our information.
		forkPoint, err := repo.ForkPoint(ctx, currentBranch, targetName)
		// TODO: don't check fork point if resolved
		if !resolved && err == nil && forkPoint != targetBranch.BaseHash {
			targetBranch.BaseHash = forkPoint
			_ = store.UpsertBranch(ctx, state.UpsertBranchRequest{
				Name:     targetName,
				BaseHash: forkPoint,
				Message:  fmt.Sprintf("%s: forked from %s", targetName, currentBranch),
			})
		}

		if targetBranch.BaseHash != currentHash {
			log.Warn().Str("branch", targetName).Msg("Branch needs to be restacked")
			log.Warn().Msg("Run 'gs branch restack' to fix")
		}
	}

	// Base hasn't changed, just checkout the child.
	if err := repo.Checkout(ctx, children[0]); err != nil {
		return fmt.Errorf("checkout %q: %w", children[0], err)
	}
	return nil
}
