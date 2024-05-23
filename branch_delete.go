package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type branchDeleteCmd struct {
	Name  string `arg:"" optional:"" help:"Name of the branch to delete" predictor:"branches"`
	Force bool   `short:"f" help:"Force deletion of the branch"`
}

func (*branchDeleteCmd) Help() string {
	return text.Dedent(`
		Deletes the specified branch and updates upstack branches to
		point to the next branch down.
	`)
}

func (cmd *branchDeleteCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
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

	svc := spice.NewService(repo, store, log)

	// TODO: prompt for branch if not provided or not an exact match
	if cmd.Name == "" {
		return errors.New("branch name is required")
	}

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	tracked, exists := true, true
	base := store.Trunk()
	if b, err := svc.LookupBranch(ctx, cmd.Name); err != nil {
		if delErr := new(spice.DeletedBranchError); errors.As(err, &delErr) {
			exists = false
			log.Info("branch has already been deleted", "branch", cmd.Name)
		} else if errors.Is(err, state.ErrNotExist) {
			tracked = false
			log.Info("branch is not tracked: deleting anyway", "branch", cmd.Name)
		} else {
			return fmt.Errorf("lookup branch %v: %w", cmd.Name, err)
		}
	} else {
		base = b.Base
	}

	// Move to the base of the branch
	// if we're on the branch we're deleting.
	if cmd.Name == currentBranch {
		if err := repo.Checkout(ctx, base); err != nil {
			return fmt.Errorf("checkout %v: %w", base, err)
		}
	}

	if exists {
		opts := git.BranchDeleteOptions{Force: cmd.Force}
		if err := repo.DeleteBranch(ctx, cmd.Name, opts); err != nil {
			// If the branch still exists,
			// it's likely because it's not merged.
			if _, peelErr := repo.PeelToCommit(ctx, cmd.Name); peelErr == nil {
				log.Error("git refused to delete the branch", "err", err)
				log.Error("try re-running with --force")
				return errors.New("branch not deleted")
			}

			// If the branch doesn't exist,
			// it may already have been deleted.
			log.Warn("branch may already have been deleted", "err", err)
		}
	}

	if tracked {
		if err := svc.ForgetBranch(ctx, cmd.Name); err != nil {
			return fmt.Errorf("forget branch %v: %w", cmd.Name, err)
		}
	}

	// TODO: flag to auto-restack upstack branches?
	return nil
}
