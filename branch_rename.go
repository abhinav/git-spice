package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
)

type branchRenameCmd struct {
	Name string `arg:"" optional:"" help:"New name of the branch"`
}

func (cmd *branchRenameCmd) Run(ctx context.Context, log *zerolog.Logger) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	// TODO: prompt for a name if not provided
	if cmd.Name == "" {
		return errors.New("branch name is required")
	}

	oldName, err := repo.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	// TODO: prompt for init if not initialized
	store, err := state.OpenStore(ctx, repo, log)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}

	if err := repo.RenameBranch(ctx, git.RenameBranchRequest{
		OldName: oldName,
		NewName: cmd.Name,
	}); err != nil {
		return fmt.Errorf("rename branch: %w", err)
	}

	// TODO: find a way to do all this atomically?
	if b, err := store.LookupBranch(ctx, oldName); err == nil {
		// TODO: perhaps we need a rename method on the store
		req := state.UpsertBranchRequest{
			Name:     cmd.Name,
			Base:     b.Base.Name,
			BaseHash: b.Base.Hash,
			Message:  fmt.Sprintf("rename %q to %q", oldName, cmd.Name),
		}
		if b.PR != 0 {
			req.PR = state.PR(b.PR)
		}

		if err := store.UpsertBranch(ctx, req); err != nil {
			return fmt.Errorf("upsert new state: %w", err)
		}

		if err := store.ForgetBranch(ctx, oldName); err != nil {
			return fmt.Errorf("forget old state: %w", err)
		}
	}

	aboves, err := store.ListAbove(ctx, oldName)
	if err != nil {
		return fmt.Errorf("list branches above: %w", err)
	}

	for _, above := range aboves {
		if err := store.UpsertBranch(ctx, state.UpsertBranchRequest{
			Name:    above,
			Base:    cmd.Name,
			Message: fmt.Sprintf("rebase %q onto %q", above, cmd.Name),
		}); err != nil {
			return fmt.Errorf("update branch %q: %w", above, err)
		}
	}

	return nil
}
