package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
)

type topCmd struct{}

func (*topCmd) Run(ctx context.Context, log *log.Logger) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	store, err := ensureStore(ctx, repo, log)
	if err != nil {
		return err
	}

	// TODO: ensure no uncommitted changes

	current, err := repo.CurrentBranch(ctx)
	if err != nil {
		// TODO: handle not a branch
		return fmt.Errorf("get current branch: %w", err)
	}

	remaining := []string{current}
	var tops []string
	for len(remaining) > 0 {
		var b string
		b, remaining = remaining[0], remaining[1:]

		aboves, err := store.ListAbove(ctx, b)
		if err != nil {
			return fmt.Errorf("list branches above %v: %w", b, err)
		}

		if len(aboves) == 0 {
			// There's nothing above this branch
			// so it's a top-most branch.
			tops = append(tops, b)
		} else {
			remaining = append(remaining, aboves...)
		}
	}
	must.NotBeEmptyf(tops, "at least current branch (%v) must be in tops", current)

	branch := tops[0]
	if len(tops) > 1 {
		log.Info("There are multiple top-level branches reachable from the current branch.")

		// If there are multiple top-most branches,
		// prompt the user to pick one.
		opts := make([]huh.Option[string], len(tops))
		for i, branch := range tops {
			opts[i] = huh.NewOption(branch, branch)
		}

		prompt := huh.NewSelect[string]().
			Title("Pick a branch").
			Options(opts...).
			Value(&branch)
		if err := prompt.Run(); err != nil {
			return fmt.Errorf("a branch is required: %w", err)
		}
	}

	if branch == current {
		log.Info("Already on the top-most branch in this stack")
		return nil
	}

	return (&checkoutCmd{Name: branch}).Run(ctx, log)
}
