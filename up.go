package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/gs"
)

type upCmd struct{}

func (*upCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
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

	aboves, err := svc.ListAbove(ctx, current)
	if err != nil {
		return fmt.Errorf("list branches above %v: %w", current, err)
	}

	var branch string
	switch len(aboves) {
	case 0:
		return fmt.Errorf("%v: no branches found upstack", current)
	case 1:
		branch = aboves[0]
	default:
		log.Info("There are multiple branches above this one.")
		if opts.NonInteractive {
			return errNonInteractive
		}

		opts := make([]huh.Option[string], len(aboves))
		for i, branch := range aboves {
			opts[i] = huh.NewOption(branch, branch)
		}

		// TODO:
		// Custom branch selection widget
		// with fuzzy search.
		prompt := huh.NewSelect[string]().
			Title("Pick a branch").
			Options(opts...).
			Value(&branch)

		if err := prompt.Run(); err != nil {
			return fmt.Errorf("a branch is required: %w", err)
		}
	}

	return (&checkoutCmd{Name: branch}).Run(ctx, log, opts)
}
