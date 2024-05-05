package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/gs"
	"go.abhg.dev/gs/internal/must"
)

type topCmd struct{}

func (*topCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	store, err := ensureStore(ctx, repo, log, opts)
	if err != nil {
		return err
	}

	svc := gs.NewService(repo, store, log)

	current, err := repo.CurrentBranch(ctx)
	if err != nil {
		// TODO: handle not a branch
		return fmt.Errorf("get current branch: %w", err)
	}

	tops, err := svc.FindTop(ctx, current)
	if err != nil {
		return fmt.Errorf("find top-most branches: %w", err)
	}
	must.NotBeEmptyf(tops, "FindTopmost always returns at least one branch")

	branch := tops[0]
	if len(tops) > 1 {
		log.Info("There are multiple top-level branches reachable from the current branch.")
		if !opts.Prompt {
			return errNoPrompt
		}

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

	return (&branchCheckoutCmd{Name: branch}).Run(ctx, log, opts)
}
