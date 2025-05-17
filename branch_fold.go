package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type branchFoldCmd struct {
	Branch string `placeholder:"NAME" help:"Name of the branch" predictor:"trackedBranches"`
}

func (*branchFoldCmd) Help() string {
	return text.Dedent(`
		Commits from the current branch will be merged into its base
		and the current branch will be deleted.
		Branches above the folded branch will point
		to the next branch downstack.
		Use the --branch flag to target a different branch.
	`)
}

func (cmd *branchFoldCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	store *state.Store,
	svc *spice.Service,
) error {
	if cmd.Branch == "" {
		currentBranch, err := repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = currentBranch
	}

	if err := svc.VerifyRestacked(ctx, cmd.Branch); err != nil {
		var restackErr *spice.BranchNeedsRestackError
		switch {
		case errors.Is(err, state.ErrNotExist):
			return fmt.Errorf("branch %v not tracked", cmd.Branch)
		case errors.As(err, &restackErr):
			return fmt.Errorf("branch %v needs to be restacked before it can be folded", cmd.Branch)
		default:
			return fmt.Errorf("verify restacked: %w", err)
		}
	}

	b, err := svc.LookupBranch(ctx, cmd.Branch)
	if err != nil {
		return fmt.Errorf("get branch: %w", err)
	}

	// Merge base into current branch using a fast-forward.
	// To do this without checking out the base, we can use a local fetch
	// and fetch the feature branch "into" the base branch.
	if err := repo.Fetch(ctx, git.FetchOptions{
		Remote: ".", // local repository
		Refspecs: []git.Refspec{
			git.Refspec(cmd.Branch + ":" + b.Base),
		},
	}); err != nil {
		return fmt.Errorf("update base branch: %w", err)
	}

	newBaseHash, err := repo.PeelToCommit(ctx, b.Base)
	if err != nil {
		return fmt.Errorf("peel to commit: %w", err)
	}

	tx := store.BeginBranchTx()

	// Change the base of all branches above us
	// to the base of the branch we are folding.
	aboves, err := svc.ListAbove(ctx, cmd.Branch)
	if err != nil {
		return fmt.Errorf("list above: %w", err)
	}

	for _, above := range aboves {
		if err := tx.Upsert(ctx, state.UpsertRequest{
			Name:     above,
			Base:     b.Base,
			BaseHash: newBaseHash,
		}); err != nil {
			return fmt.Errorf("set base of %v to %v: %w", above, b.Base, err)
		}
		log.Debug("Changing base branch of upstream",
			"branch", above,
			"newBase", b.Base)
	}

	if err := tx.Delete(ctx, cmd.Branch); err != nil {
		return fmt.Errorf("delete branch %v from state: %w", cmd.Branch, err)
	}
	log.Debug("Removing branch from tracking", "branch", cmd.Branch)

	if err := tx.Commit(ctx, fmt.Sprintf("folding %v into %v", cmd.Branch, b.Base)); err != nil {
		return fmt.Errorf("update state: %w", err)
	}

	// Check out base and delete the branch we are folding.
	if err := (&branchCheckoutCmd{Branch: b.Base}).Run(
		ctx, log, view, repo, store, svc,
	); err != nil {
		return fmt.Errorf("checkout base: %w", err)
	}

	if err := repo.DeleteBranch(ctx, cmd.Branch, git.BranchDeleteOptions{
		Force: true, // we know it's merged
	}); err != nil {
		return fmt.Errorf("delete branch: %w", err)
	}

	log.Infof("Branch %v has been folded into %v", cmd.Branch, b.Base)
	return nil
}
