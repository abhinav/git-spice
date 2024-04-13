package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
)

type branchRenameCmd struct {
	Name string `arg:"" optional:"" help:"New name of the branch"`
}

func (cmd *branchRenameCmd) Run(ctx context.Context, log *log.Logger) (err error) {
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

	store, err := ensureStore(ctx, repo, log)
	if err != nil {
		return err
	}

	if err := repo.RenameBranch(ctx, git.RenameBranchRequest{
		OldName: oldName,
		NewName: cmd.Name,
	}); err != nil {
		return fmt.Errorf("rename branch: %w", err)
	}

	var upserts []state.UpsertBranchRequest
	if b, err := store.LookupBranch(ctx, oldName); err == nil {
		req := state.UpsertBranchRequest{
			Name:     cmd.Name,
			Base:     b.Base.Name,
			BaseHash: b.Base.Hash,
		}
		if b.PR != 0 {
			req.PR = state.PR(b.PR)
		}

		upserts = append(upserts, req)
		defer func() {
			if err != nil {
				return
			}
			// Delete state only if the rest of the operation is successful.

			err = store.ForgetBranch(ctx, oldName)
			if err != nil {
				err = fmt.Errorf("forget old state: %w", err)
			}
		}()
	}

	aboves, err := store.ListAbove(ctx, oldName)
	if err != nil {
		return fmt.Errorf("list branches above: %w", err)
	}

	for _, above := range aboves {
		upserts = append(upserts, state.UpsertBranchRequest{
			Name: above,
			Base: cmd.Name,
		})
	}

	if err := store.UpsertBranches(ctx, upserts, fmt.Sprintf("rename %q to %q", oldName, cmd.Name)); err != nil {
		return fmt.Errorf("update branches: %w", err)
	}

	return nil
}
