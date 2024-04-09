package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/state"
)

type branchDeleteCmd struct {
	Name  string `arg:"" optional:"" help:"Name of the branch to delete"`
	Force bool   `short:"f" help:"Force deletion of the branch"`
}

func (cmd *branchDeleteCmd) Run(ctx context.Context, log *log.Logger) error {
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

	// TODO: prompt for branch if not provided or not an exact match
	if cmd.Name == "" {
		return errors.New("branch name is required")
	}

	var (
		aboves []string
		base   string
	)
	if b, err := store.LookupBranch(ctx, cmd.Name); err == nil {
		// If we know this branch, we'll want to update the upstacks
		// after deletion.
		base = b.Base.Name

		aboves, err = store.ListAbove(ctx, cmd.Name)
		if err != nil {
			log.Warn("failed to list branches above", "branch", cmd.Name, "err", err)
			aboves = nil
		}
	} else {
		log.Warn("branch is not tracked by gs; deleting anyway", "branch", cmd.Name)
	}

	if err := repo.DeleteBranch(ctx, cmd.Name, git.BranchDeleteOptions{
		Force: cmd.Force,
	}); err != nil {
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

	if err := store.ForgetBranch(ctx, cmd.Name); err != nil {
		log.Warn("Could not delete branch from store.", "err", err)
	}

	if len(aboves) == 0 {
		return nil
	}

	must.NotBeBlankf(base, "base must be set if branches were found above")
	for _, above := range aboves {
		if err := store.UpsertBranch(ctx, state.UpsertBranchRequest{
			Name: above,
			Base: base,
		}); err != nil {
			return fmt.Errorf("update upstack: %w", err)
		}
	}

	// TODO: auto-restack with opt-out flag
	return nil
}
