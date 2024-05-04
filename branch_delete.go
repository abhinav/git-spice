package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/gs"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/state"
)

type branchDeleteCmd struct {
	Name  string `arg:"" optional:"" help:"Name of the branch to delete"`
	Force bool   `short:"f" help:"Force deletion of the branch"`
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

	svc := gs.NewService(repo, store, log)

	// TODO: prompt for branch if not provided or not an exact match
	if cmd.Name == "" {
		return errors.New("branch name is required")
	}

	var (
		aboves  []string
		base    string
		tracked bool
	)
	if b, err := store.Lookup(ctx, cmd.Name); err == nil {
		tracked = true
		// If we know this branch, we'll want to update the upstacks
		// after deletion.
		base = b.Base

		aboves, err = svc.ListAbove(ctx, cmd.Name)
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

	if !tracked {
		return nil
	}

	update := state.UpdateRequest{
		Message: fmt.Sprintf("delete branch %v", cmd.Name),
		Deletes: []string{cmd.Name},
	}

	if len(aboves) > 0 {
		must.NotBeBlankf(base, "base must be set if branches were found above")
	}
	for _, above := range aboves {
		update.Upserts = append(update.Upserts, state.UpsertRequest{
			Name: above,
			Base: base,
		})
	}

	if err := store.Update(ctx, &update); err != nil {
		return fmt.Errorf("update branches: %w", err)
	}

	// TODO: auto-restack with opt-out flag
	return nil
}
