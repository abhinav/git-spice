package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/gs"
	"go.abhg.dev/gs/internal/state"
)

type branchRenameCmd struct {
	Name string `arg:"" optional:"" help:"New name of the branch"`
}

func (cmd *branchRenameCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) (err error) {
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

	store, err := ensureStore(ctx, repo, log, opts)
	if err != nil {
		return err
	}

	svc := gs.NewService(repo, store, log)

	// TODO: Check if cmd.Name already exists.
	if err := repo.RenameBranch(ctx, git.RenameBranchRequest{
		OldName: oldName,
		NewName: cmd.Name,
	}); err != nil {
		return fmt.Errorf("rename branch: %w", err)
	}

	// TODO:
	// If branch has a PR, we'll want to retain the upstream branch name.
	// Maybe 'branch submit' should track the upstream branch name.
	update := state.UpdateRequest{
		Message: fmt.Sprintf("rename %q to %q", oldName, cmd.Name),
	}
	if b, err := store.Lookup(ctx, oldName); err == nil {
		req := state.UpsertRequest{
			Name:     cmd.Name,
			Base:     b.Base,
			BaseHash: b.BaseHash,
		}

		update.Upserts = append(update.Upserts, req)
		update.Deletes = append(update.Deletes, oldName)
	}

	aboves, err := svc.ListAbove(ctx, oldName)
	if err != nil {
		return fmt.Errorf("list branches above: %w", err)
	}

	for _, above := range aboves {
		update.Upserts = append(update.Upserts, state.UpsertRequest{
			Name: above,
			Base: cmd.Name,
		})
	}

	if err := store.Update(ctx, &update); err != nil {
		return fmt.Errorf("update branches: %w", err)
	}

	return nil
}
