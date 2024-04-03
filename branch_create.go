package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
)

type branchCreateCmd struct {
	Name string `arg:"" optional:"" help:"Name of the new branch"`

	Message string `short:"m" long:"message" optional:"" help:"Commit message"`
}

func (cmd *branchCreateCmd) Run(ctx context.Context, log *log.Logger) (err error) {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	// TODO: prompt for init if not initialized
	store, err := state.OpenStore(ctx, repo)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}

	// TODO: guess branch name from commit name
	if cmd.Name == "" {
		return errors.New("branch name is required")
	}

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	currentHash, err := repo.PeelToCommit(ctx, "HEAD")
	if err != nil {
		return fmt.Errorf("peel to tree: %w", err)
	}

	diff, err := repo.DiffIndex(ctx, currentHash.String())
	if err != nil {
		return fmt.Errorf("diff index: %w", err)
	}

	if err := repo.DetachHead(ctx); err != nil {
		return fmt.Errorf("detach head: %w", err)
	}
	// From this point on, if there's an error,
	// restore the original branch.
	defer func() {
		if err != nil {
			err = errors.Join(err, repo.Checkout(ctx, currentBranch))
		}
	}()

	if err := repo.Commit(ctx, git.CommitRequest{
		AllowEmpty: len(diff) == 0,
		Message:    cmd.Message,
	}); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	if err := repo.CreateBranch(ctx, git.CreateBranchRequest{
		Name: cmd.Name,
		Head: "HEAD",
	}); err != nil {
		return fmt.Errorf("create branch: %w", err)
	}

	if err := repo.Checkout(ctx, cmd.Name); err != nil {
		return fmt.Errorf("checkout branch: %w", err)
	}

	if err := store.UpsertBranch(ctx, state.UpsertBranchRequest{
		Name:     cmd.Name,
		Base:     currentBranch,
		BaseHash: currentHash,
		Message:  fmt.Sprintf("create branch %s on %s", cmd.Name, currentBranch),
	}); err != nil {
		return fmt.Errorf("set branch: %w", err)
	}

	return nil
}
