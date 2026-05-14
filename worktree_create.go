package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type worktreeCreateCmd struct {
	Path   string `arg:"" help:"Path for the new worktree"`
	Branch string `short:"b" placeholder:"BRANCH" help:"Create and check out a new branch in the worktree"`
}

func (*worktreeCreateCmd) Help() string {
	return text.Dedent(`
		Creates a new Git worktree at the given path.
		The worktree starts in detached HEAD state
		at the current trunk commit.

		Use -b/--branch to create a new tracked branch
		in the worktree.
	`)
}

func (cmd *worktreeCreateCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	repo *git.Repository,
	store *state.Store,
) error {
	trunk := store.Trunk()

	trunkHash, err := repo.PeelToCommit(ctx, trunk)
	if err != nil {
		return fmt.Errorf("resolve %v: %w", trunk, err)
	}

	// Create worktree in detached HEAD state at trunk.
	if err := repo.WorktreeAdd(ctx, git.WorktreeAddRequest{
		Path:   cmd.Path,
		Detach: true,
		Head:   trunkHash.String(),
	}); err != nil {
		return fmt.Errorf("create worktree: %w", err)
	}

	log.Infof("Created worktree at %s", cmd.Path)

	if cmd.Branch != "" {
		// Open the new worktree and create a branch.
		newWT, err := repo.OpenWorktree(ctx, cmd.Path)
		if err != nil {
			return fmt.Errorf("open worktree: %w", err)
		}

		if err := repo.CreateBranch(ctx, git.CreateBranchRequest{
			Name: cmd.Branch,
			Head: trunkHash.String(),
		}); err != nil {
			return fmt.Errorf("create branch: %w", err)
		}

		if err := newWT.CheckoutBranch(ctx, cmd.Branch); err != nil {
			return fmt.Errorf("checkout branch: %w", err)
		}

		log.Infof("Created and checked out branch %s", cmd.Branch)
	}

	return nil
}
