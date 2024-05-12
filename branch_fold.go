package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/gs"
	"go.abhg.dev/gs/internal/state"
	"go.abhg.dev/gs/internal/text"
)

type branchFoldCmd struct {
	Name string `arg:"" optional:"" help:"Name of the branch"`
}

func (*branchFoldCmd) Help() string {
	return text.Dedent(`
		Merges the changes of a branch into its base branch
		and deletes it.
		Branches above the folded branch will be restacked
		on top of the base branch.
	`)
}

func (cmd *branchFoldCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	store, err := ensureStore(ctx, repo, log, opts)
	if err != nil {
		return err
	}

	svc := gs.NewService(repo, store, log)

	if cmd.Name == "" {
		currentBranch, err := repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Name = currentBranch
	}

	if err := svc.VerifyRestacked(ctx, cmd.Name); err != nil {
		switch {
		case errors.Is(err, gs.ErrNotExist):
			return fmt.Errorf("branch %v not tracked", cmd.Name)
		case errors.Is(err, gs.ErrNeedsRestack):
			return fmt.Errorf("branch %v needs to be restacked before it can be folded", cmd.Name)
		default:
			return fmt.Errorf("verify restacked: %w", err)
		}
	}

	b, err := store.Lookup(ctx, cmd.Name)
	if err != nil {
		return fmt.Errorf("get branch: %w", err)
	}

	aboves, err := svc.ListAbove(ctx, cmd.Name)
	if err != nil {
		return fmt.Errorf("list above: %w", err)
	}

	// Merge base into current branch using a fast-forward.
	// To do this without checking out the base, we can use a local fetch
	// and fetch the feature branch "into" the base branch.
	if err := repo.Fetch(ctx, git.FetchOptions{
		Remote: ".", // local repository
		Refspecs: []git.Refspec{
			git.Refspec(cmd.Name + ":" + b.Base),
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
	upserts := make([]state.UpsertRequest, len(aboves))
	for i, above := range aboves {
		upserts[i] = state.UpsertRequest{
			Name:     above,
			Base:     b.Base,
			BaseHash: newBaseHash,
		}
	}

	err = store.Update(ctx, &state.UpdateRequest{
		Upserts: upserts,
		Deletes: []string{cmd.Name},
		Message: fmt.Sprintf("folding %v into %v", cmd.Name, b.Base),
	})
	if err != nil {
		return fmt.Errorf("upsert branches: %w", err)
	}

	// Check out base and delete the branch we are folding.
	if err := (&branchCheckoutCmd{Name: b.Base}).Run(ctx, log, opts); err != nil {
		return fmt.Errorf("checkout base: %w", err)
	}

	if err := repo.DeleteBranch(ctx, cmd.Name, git.BranchDeleteOptions{
		Force: true, // we know it's merged
	}); err != nil {
		return fmt.Errorf("delete branch: %w", err)
	}

	log.Infof("Branch %v has been folded into %v", cmd.Name, b.Base)
	return nil
}
