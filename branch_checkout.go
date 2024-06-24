package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type branchCheckoutCmd struct {
	Untracked bool   `short:"u" help:"Show untracked branches if one isn't supplied"`
	Branch    string `arg:"" optional:"" help:"Name of the branch to delete" predictor:"branches"`
}

func (*branchCheckoutCmd) Help() string {
	return text.Dedent(`
		A prompt will allow selecting between tracked branches.
		Provide a branch name as an argument to skip the prompt.
		Use -u/--untracked to show untracked branches in the prompt.
	`)
}

func (cmd *branchCheckoutCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, store, svc, err := openRepo(ctx, log, opts)
	if err != nil {
		return err
	}

	if cmd.Branch == "" {
		if !opts.Prompt {
			return fmt.Errorf("cannot proceed without a branch name: %w", errNoPrompt)
		}

		// If a branch name is not provided,
		// list branches besides the current branch and pick one.
		currentBranch, err := repo.CurrentBranch(ctx)
		if err != nil {
			currentBranch = ""
		}

		cmd.Branch, err = (&branchPrompt{
			Exclude:           []string{currentBranch},
			ExcludeCheckedOut: true,
			TrackedOnly:       !cmd.Untracked,
			Title:             "Select a branch to checkout",
		}).Run(ctx, repo, store)
		if err != nil {
			return fmt.Errorf("select branch: %w", err)
		}
	}

	if err := svc.VerifyRestacked(ctx, cmd.Branch); err != nil {
		var restackErr *spice.BranchNeedsRestackError
		switch {
		case errors.As(err, &restackErr):
			log.Warnf("%v: needs to be restacked: run 'gs branch restack %v'", cmd.Branch, cmd.Branch)
		case errors.Is(err, state.ErrNotExist):
			if store.Trunk() != cmd.Branch {
				if !opts.Prompt {
					log.Warnf("%v: branch not tracked: run 'gs branch track'", cmd.Branch)
				} else {
					log.Warnf("%v: branch not tracked", cmd.Branch)
					track := true
					prompt := ui.NewConfirm().
						WithValue(&track).
						WithTitle("Do you want to track this branch now?")
					if err := ui.Run(prompt); err != nil {
						return fmt.Errorf("prompt: %w", err)
					}

					if track {
						err := (&branchTrackCmd{
							Branch: cmd.Branch,
						}).Run(ctx, log, opts)
						if err != nil {
							return fmt.Errorf("track branch: %w", err)
						}
					}

				}
			}
		case errors.Is(err, git.ErrNotExist):
			return fmt.Errorf("branch %q does not exist", cmd.Branch)
		default:
			log.Warnf("error checking branch: %v", err)
		}
	}

	if err := repo.Checkout(ctx, cmd.Branch); err != nil {
		return fmt.Errorf("checkout %q: %w", cmd.Branch, err)
	}

	return nil
}
