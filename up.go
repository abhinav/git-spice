package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/rs/zerolog"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/state"
)

type upCmd struct{}

func (*upCmd) Run(ctx context.Context, log *zerolog.Logger) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	// TODO: prompt for init if not initialized
	store, err := state.OpenStore(ctx, repo, log)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}

	// TODO: ensure no uncommitted changes

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	currentHash, err := repo.PeelToCommit(ctx, currentBranch)
	if err != nil {
		return fmt.Errorf("peel to commit of %q: %w", currentBranch, err)
	}

	children, err := store.ListAbove(ctx, currentBranch)
	if err != nil {
		return fmt.Errorf("list children of %q: %w", currentBranch, err)
	}

	var targetName string
	switch len(children) {
	case 0:
		return fmt.Errorf("%v: no branches found upstack", currentBranch)
	case 1:
		targetName = children[0]
	default:
		opts := make([]huh.Option[string], len(children))
		for i, child := range children {
			opts[i] = huh.NewOption(child, child)
		}

		// TODO:
		// Custom branch selection widget
		// with fuzzy search.
		prompt := huh.NewSelect[string]().
			Title("Pick a branch").
			Options(opts...).
			Value(&targetName)

		if err := prompt.Run(); err != nil {
			return fmt.Errorf("a branch is required: %w", err)
		}
	}

	targetBranch, err := store.LookupBranch(ctx, targetName)
	if err != nil {
		return fmt.Errorf("get branch %q: %w", children[0], err)
	}

	if targetBranch.Base.Hash != currentHash {
		log.Warn().Str("branch", targetName).Msg("Branch needs to be restacked")
		log.Warn().Msg("Run 'gs branch restack' to fix")
	}

	// Base hasn't changed, just checkout the child.
	if err := repo.Checkout(ctx, children[0]); err != nil {
		return fmt.Errorf("checkout %q: %w", children[0], err)
	}
	return nil
}
