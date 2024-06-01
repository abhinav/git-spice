package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/ui"
)

type branchCheckoutCmd struct {
	Untracked bool   `short:"u" help:"Show untracked branches if one isn't supplied"`
	Name      string `arg:"" optional:"" help:"Name of the branch to delete" predictor:"branches"`
}

func (cmd *branchCheckoutCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
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

	if cmd.Name == "" {
		if !opts.Prompt {
			return fmt.Errorf("cannot proceed without a branch name: %w", errNoPrompt)
		}

		// If a branch name is not provided,
		// list branches besides the current branch and pick one.
		currentBranch, err := repo.CurrentBranch(ctx)
		if err != nil {
			currentBranch = ""
		}

		cmd.Name, err = (&branchPrompt{
			Exclude:           []string{currentBranch},
			ExcludeCheckedOut: true,
			TrackedOnly:       !cmd.Untracked,
			Title:             "Select a branch to checkout",
		}).Run(ctx, repo, store)
		if err != nil {
			return fmt.Errorf("select branch: %w", err)
		}
	}

	if err := svc.VerifyRestacked(ctx, cmd.Name); err != nil {
		var restackErr *spice.BranchNeedsRestackError
		switch {
		case errors.As(err, &restackErr):
			log.Warnf("%v: needs to be restacked: run 'gs branch restack %v'", cmd.Name, cmd.Name)
		case errors.Is(err, state.ErrNotExist):
			if store.Trunk() != cmd.Name {
				if !opts.Prompt {
					log.Warnf("%v: branch not tracked: run 'gs branch track'", cmd.Name)
				} else {
					log.Warnf("%v: branch not tracked", cmd.Name)
					track := true
					prompt := ui.NewConfirm().
						WithValue(&track).
						WithTitle("Do you want to track this branch now?")
					if err := ui.Run(prompt); err != nil {
						return fmt.Errorf("prompt: %w", err)
					}

					if track {
						err := (&branchTrackCmd{
							Name: cmd.Name,
						}).Run(ctx, log, opts)
						if err != nil {
							return fmt.Errorf("track branch: %w", err)
						}
					}

				}
			}
		case errors.Is(err, git.ErrNotExist):
			return fmt.Errorf("branch %q does not exist", cmd.Name)
		default:
			log.Warnf("error checking branch: %v", err)
		}
	}

	if err := repo.Checkout(ctx, cmd.Name); err != nil {
		return fmt.Errorf("checkout %q: %w", cmd.Name, err)
	}

	return nil
}
