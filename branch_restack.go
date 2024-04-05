package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
)

type branchRestackCmd struct {
	Name string `arg:"" optional:"" help:"Branch to restack. Defaults to the current branch."`
}

func (cmd *branchRestackCmd) Run(ctx context.Context, log *zerolog.Logger) error {
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

	if cmd.Name == "" {
		currentBranch, err := repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Name = currentBranch
	}

	head := cmd.Name
	b, err := store.LookupBranch(ctx, head)
	if err != nil {
		return fmt.Errorf("get branch: %w", err)
	}

	actualBaseHash, err := repo.PeelToCommit(ctx, b.Base.Name)
	if err != nil {
		// TODO:
		// Base branch has been deleted.
		// Find the parent of the base branch
		// and use that as the new base.
		if errors.Is(err, git.ErrNotExist) {
			return fmt.Errorf("base branch %v does not exist", b.Base.Name)
		}
		return fmt.Errorf("peel to commit: %w", err)
	}

	if actualBaseHash == b.Base.Hash {
		log.Info().Msgf("Branch %v does not need to be restacked.", head)
		return nil
	}

	// Case:
	// The user has already fixed the branch.
	// Our information is stale, and we just need to update that.
	mergeBase, err := repo.MergeBase(ctx, b.Base.Name, head)
	if err == nil && mergeBase == actualBaseHash {
		err := store.UpsertBranch(ctx, state.UpsertBranchRequest{
			Name:     head,
			BaseHash: mergeBase,
			Message:  fmt.Sprintf("%s: rebased externally on %s", head, b.Base.Name),
		})
		if err != nil {
			return fmt.Errorf("update branch information: %w", err)
		}
		log.Info().Msgf("Branch %v was already restacked on %v", head, b.Base.Name)
		return nil
	}

	rebaseFrom := b.Base.Hash
	// Case:
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
	forkPoint, err := repo.ForkPoint(ctx, b.Base.Name, head)
	if err == nil {
		rebaseFrom = forkPoint
		log.Debug().Msgf("Using fork point %v as rebase base", rebaseFrom)
	}

	if err := repo.Rebase(ctx, git.RebaseRequest{
		Onto:     actualBaseHash.String(),
		Upstream: rebaseFrom.String(),
		Branch:   head,
		Quiet:    true,
	}); err != nil {
		return fmt.Errorf("rebase: %w", err)
		// TODO: detect conflicts in rebase,
		// print message about "gs continue"
	}

	if err := store.UpsertBranch(ctx, state.UpsertBranchRequest{
		Name:     head,
		BaseHash: actualBaseHash,
		Message:  fmt.Sprintf("%s: restacked on %s", head, b.Base.Name),
	}); err != nil {
		return fmt.Errorf("update branch information: %w", err)
	}

	log.Info().Msgf("Branch %v restacked on %v", head, b.Base.Name)
	return nil
}
