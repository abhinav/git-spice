package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
)

type branchRestackCmd struct {
	Name string `arg:"" optional:"" help:"Branch to restack. Defaults to the current branch."`

	// Hack:
	// When invoked from another operation (e.g. commit create)
	// we don't want to report that the base of that operation
	// does not need to be restacked
	// because that will always be true.
	// To avoid that annoying message, we'll use this private field
	// to inject that branch name.
	noLogUpToDate string
	// TODO: when internal state is shared with another abstraction
	// this should not be necessary
}

func (cmd *branchRestackCmd) Run(ctx context.Context, log *log.Logger) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	store, err := ensureStore(ctx, repo, log)
	if err != nil {
		return err
	}

	if cmd.Name == "" {
		currentBranch, err := repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Name = currentBranch
	}

	head := cmd.Name
	b, err := store.Lookup(ctx, head)
	if err != nil {
		return fmt.Errorf("get branch: %w", err)
	}

	actualBaseHash, err := repo.PeelToCommit(ctx, b.Base)
	if err != nil {
		// TODO:
		// Base branch has been deleted.
		// Suggest a means of repairing this:
		// possibly by prompting to select a different base branch.
		if errors.Is(err, git.ErrNotExist) {
			return fmt.Errorf("base branch %v does not exist", b.Base)
		}
		return fmt.Errorf("peel to commit: %w", err)
	}

	// Case:
	// The user has already fixed the branch.
	// Our information is stale, and we just need to update that.
	mergeBase, err := repo.MergeBase(ctx, b.Base, head)
	if err == nil && mergeBase == actualBaseHash {
		if mergeBase != b.BaseHash {
			err := store.Update(ctx, &state.UpdateRequest{
				Upserts: []state.UpsertRequest{
					{
						Name:     head,
						BaseHash: mergeBase,
					},
				},
				Message: fmt.Sprintf("%s: rebased externally on %s", head, b.Base),
			})
			if err != nil {
				return fmt.Errorf("update branch information: %w", err)
			}
		}
		if head != cmd.noLogUpToDate {
			log.Infof("Branch %v does not need to be restacked.", head)
		}
		return nil
	}

	rebaseFrom := b.BaseHash
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
	forkPoint, err := repo.ForkPoint(ctx, b.Base, head)
	if err == nil {
		rebaseFrom = forkPoint
		log.Debugf("Using fork point %v as rebase base", rebaseFrom)
	}

	if err := repo.Rebase(ctx, git.RebaseRequest{
		Onto:      actualBaseHash.String(),
		Upstream:  rebaseFrom.String(),
		Branch:    head,
		Autostash: true,
		Quiet:     true,
	}); err != nil {
		return fmt.Errorf("rebase: %w", err)
		// TODO: detect conflicts in rebase,
		// print message about "gs continue"
	}

	err = store.Update(ctx, &state.UpdateRequest{
		Upserts: []state.UpsertRequest{
			{
				Name:     head,
				BaseHash: actualBaseHash,
			},
		},
		Message: fmt.Sprintf("%s: restacked on %s", head, b.Base),
	})
	if err != nil {
		return fmt.Errorf("update branch information: %w", err)
	}

	log.Infof("Branch %v restacked on %v", head, b.Base)
	return nil
}

func checkNeedsRestack(ctx context.Context, repo *git.Repository, store *state.Store, log *log.Logger, name string) {
	// A branch needs to be restacked if
	// a) it's tracked by gs; and
	// b) its merge base with its base branch
	//    is not its base branch's head
	b, err := store.Lookup(ctx, name)
	if err != nil {
		return // probably not tracked
	}
	mergeBase, err := repo.MergeBase(ctx, name, b.Base)
	if err != nil {
		log.Warn("Could not look up merge base for branch with its base",
			"branch", name,
			"base", b.Base,
			"err", err)
		return
	}

	baseHash, err := repo.PeelToCommit(ctx, b.Base)
	if err != nil {
		log.Warnf("%v: base branch %v may not exist: %v", name, b.Base, err)
		return
	}

	if baseHash != mergeBase {
		log.Warnf("%v: needs to be restacked: run 'gs branch restack %v'", name, name)
	}
}
