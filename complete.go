package main

import (
	"context"
	"time"

	"github.com/posener/complete"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/komplete"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type completeCmd struct {
	*komplete.Command `embed:""`
}

func (c *completeCmd) Help() string {
	return text.Dedent(`
		Generates shell completion scripts for git-spice.
		To install the script, add the generated script to your shell's
		rc file. For example:

			# bash
			gs complete bash >> ~/.bashrc

			# zsh
			gs complete zsh >> ~/.zshrc

			# fish
			gs complete fish >> ~/.config/fish/config.fish
	`)
}

func predictBranches(args complete.Args) (predictions []string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	repo, err := git.Open(ctx, ".", git.OpenOptions{})
	if err != nil {
		return nil
	}

	branches, err := repo.LocalBranches(ctx)
	if err != nil {
		return nil
	}

	for _, branch := range branches {
		predictions = append(predictions, branch.Name)
	}

	return predictions
}

func predictTrackedBranches(args complete.Args) (predictions []string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	repo, err := git.Open(ctx, ".", git.OpenOptions{})
	if err != nil {
		return nil
	}

	store, err := state.OpenStore(ctx, repo, nil /* log */)
	if err != nil {
		return nil // not initialized
	}

	branches, err := store.List(ctx)
	if err != nil {
		return nil
	}

	return branches
}

func predictRemotes(args complete.Args) (predictions []string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	repo, err := git.Open(ctx, ".", git.OpenOptions{})
	if err != nil {
		return nil
	}

	remotes, err := repo.ListRemotes(ctx)
	if err != nil {
		return nil
	}

	return remotes
}
