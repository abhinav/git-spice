package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type topCmd struct {
	DryRun bool `short:"n" help:"Print the target branch without checking it out."`
}

func (*topCmd) Help() string {
	return text.Dedent(`
		Jumps to the top-most branch in the current branch's stack.
		If there are multiple top-most branches,
		you will be prompted to pick one.
	`)
}

func (cmd *topCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, _, svc, err := openRepo(ctx, log, opts)
	if err != nil {
		return err
	}

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
		desc := "There are multiple top-level branches reachable from the current branch."
		if !opts.Prompt {
			log.Error(desc)
			return errNoPrompt
		}

		// If there are multiple top-most branches,
		// prompt the user to pick one.
		prompt := ui.NewSelect().
			WithValue(&branch).
			WithOptions(tops...).
			WithTitle("Pick a branch").
			WithDescription(desc)
		if err := ui.Run(prompt); err != nil {
			return fmt.Errorf("a branch is required: %w", err)
		}
	}

	if cmd.DryRun {
		fmt.Println(branch)
		return nil
	}

	if branch == current {
		log.Info("Already on the top-most branch in this stack")
		return nil
	}

	return (&branchCheckoutCmd{Name: branch}).Run(ctx, log, opts)
}
