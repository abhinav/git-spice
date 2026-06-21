package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type repoRestoreCmd struct{}

func (*repoRestoreCmd) Help() string {
	return text.Dedent(`
		Leaves exclusive mode: the worktrees recorded by 'gs repo park'
		are re-created at their branches' current tips, and the
		exclusive-mode marker is cleared.

		It is idempotent and resumable: worktrees that already exist are
		left alone, so an interrupted restore can be finished by
		re-running the command.
	`)
}

func (cmd *repoRestoreCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	repo *git.Repository,
	store *state.Store,
) error {
	if !store.InExclusiveMode() {
		log.Infof("Repository is not in exclusive mode; nothing to restore")
		return nil
	}
	parked := store.ParkedWorktrees()

	// Skip worktrees that already exist so a re-run after a crash only
	// re-creates what is missing.
	existing := make(map[string]bool)
	for item, err := range repo.Worktrees(ctx) {
		if err != nil {
			return fmt.Errorf("list worktrees: %w", err)
		}
		existing[item.Path] = true
	}

	for _, p := range parked {
		if existing[p.Path] {
			continue
		}

		req := git.WorktreeAddRequest{Path: p.Path}
		if p.Branch != "" {
			req.Head = p.Branch
		} else {
			req.Detach = true
		}
		if err := repo.WorktreeAdd(ctx, req); err != nil {
			return fmt.Errorf("restore worktree %q: %w", p.Path, err)
		}
		log.Infof("Restored worktree %s", p.Path)
	}

	if err := store.Unpark(ctx); err != nil {
		return fmt.Errorf("leave exclusive mode: %w", err)
	}
	log.Infof("Restored %d worktree(s); exclusive mode cleared", len(parked))
	return nil
}
