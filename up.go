package main

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/text"
)

type upCmd struct {
	N int `arg:"" optional:"" help:"Number of branches to move up." default:"1"`
}

func (*upCmd) Help() string {
	return text.Dedent(`
		Moves up the stack to the branch on top of the current one.
		If there are multiple branches with the current branch as base,
		you will be prompted to pick one.
	`)
}

func (cmd *upCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
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

	svc := spice.NewService(repo, store, log)

	current, err := repo.CurrentBranch(ctx)
	if err != nil {
		// TODO: handle not a branch
		return fmt.Errorf("get current branch: %w", err)
	}

	var branch string
outer:
	for range cmd.N {
		aboves, err := svc.ListAbove(ctx, current)
		if err != nil {
			return fmt.Errorf("list branches above %v: %w", current, err)
		}

		switch len(aboves) {
		case 0:
			if branch != "" {
				// If we've already moved up once,
				// and there are no branches above the current one,
				// we're done.
				break outer
			}
			return fmt.Errorf("%v: no branches found upstack", current)
		case 1:
			branch = aboves[0]
		default:
			log.Info("There are multiple branches above this one.")
			if !opts.Prompt {
				return errNoPrompt
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

			err := huh.NewForm(huh.NewGroup(prompt)).
				WithOutput(os.Stdout).
				WithShowHelp(false).
				Run()
			if err != nil {
				return fmt.Errorf("a branch is required: %w", err)
			}
		}

		current = branch
	}

	return (&branchCheckoutCmd{Name: branch}).Run(ctx, log, opts)
}
