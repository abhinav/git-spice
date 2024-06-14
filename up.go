package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type upCmd struct {
	N int `arg:"" optional:"" help:"Number of branches to move up." default:"1"`

	DryRun bool `short:"n" help:"Print the target branch without checking it out."`
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
			desc := "There are multiple branches above the current branch."
			if !opts.Prompt {
				log.Error(desc)
				return errNoPrompt
			}

			prompt := ui.NewSelect().
				WithValue(&branch).
				WithOptions(aboves...).
				WithTitle("Pick a branch").
				WithDescription(desc)

			if err := ui.Run(prompt); err != nil {
				return fmt.Errorf("a branch is required: %w", err)
			}
		}

		current = branch
	}

	if cmd.DryRun {
		fmt.Println(branch)
		return nil
	}

	return (&branchCheckoutCmd{Name: branch}).Run(ctx, log, opts)
}
