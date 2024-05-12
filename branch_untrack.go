package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
	"go.abhg.dev/gs/internal/text"
)

type branchUntrackCmd struct {
	Name string `arg:"" optional:"" help:"Name of the branch to untrack"`
}

func (*branchUntrackCmd) Help() string {
	return text.Dedent(`
		Removes information about a tracked branch from gs.
		Use this to forget about branches that were deleted outside gs,
		or those that are no longer relevant.
	`)
}

func (cmd *branchUntrackCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	if cmd.Name == "" {
		cmd.Name, err = repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
	}

	store, err := ensureStore(ctx, repo, log, opts)
	if err != nil {
		return err
	}

	if _, err := store.Lookup(ctx, cmd.Name); err != nil {
		return fmt.Errorf("branch already not tracked: %w", err)
	}

	// TODO: reject if there are upstream branches.

	// TODO: prompt for confirmation
	err = store.Update(ctx, &state.UpdateRequest{
		Deletes: []string{cmd.Name},
		Message: fmt.Sprintf("untrack branch %q", cmd.Name),
	})
	if err != nil {
		return fmt.Errorf("forget branch: %w", err)
	}

	return nil
}
