package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
)

type branchFoldCmd struct {
	Name string `arg:"" optional:"" help:"Name of the branch"`
}

func (cmd *branchFoldCmd) Run(ctx context.Context, log *log.Logger) error {
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

	// TODO: check that the branch does not need restacking
	b, err := store.LookupBranch(ctx, cmd.Name)
	if err != nil {
		return fmt.Errorf("get branch: %w", err)
	}

	aboves, err := store.ListAbove(ctx, cmd.Name)
	if err != nil {
		return fmt.Errorf("list above: %w", err)
	}

	// Merge base into current branch using a fast-forward.
	// To do this without checking out the base, we can use a local fetch
	// and fetch the feature branch "into" the base branch.
	if err := repo.Fetch(ctx, git.FetchOptions{
		Remote: ".", // local repository
		Refspecs: []string{
			cmd.Name + ":" + b.Base,
		},
	}); err != nil {
		return fmt.Errorf("update base branch: %w", err)
	}

	newBaseHash, err := repo.PeelToCommit(ctx, b.Base)
	if err != nil {
		return fmt.Errorf("peel to commit: %w", err)
	}

	// Change the base of all branches above us
	// to the base of the branch we are folding.
	upserts := make([]state.UpsertBranchRequest, len(aboves))
	for i, above := range aboves {
		upserts[i] = state.UpsertBranchRequest{
			Name:     above,
			Base:     b.Base,
			BaseHash: newBaseHash,
		}
	}

	err = store.Update(ctx, &state.UpdateRequest{
		Upserts: upserts,
		Message: fmt.Sprintf("folding %v into %v", cmd.Name, b.Base),
	})
	if err != nil {
		return fmt.Errorf("upsert branches: %w", err)
	}

	// Check out base and delete the branch we are folding.
	if err := (&checkoutCmd{Name: b.Base}).Run(ctx, log); err != nil {
		return fmt.Errorf("checkout base: %w", err)
	}

	if err := (&branchDeleteCmd{Name: cmd.Name, Force: true}).Run(ctx, log); err != nil {
		return fmt.Errorf("delete branch %q: %w", cmd.Name, err)
	}

	log.Infof("Branch %v has been folded into %v", cmd.Name, b.Base)
	return nil
}
