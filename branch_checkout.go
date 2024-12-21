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

type checkoutOptions struct {
	DryRun bool `short:"n" xor:"detach-or-dry-run" help:"Print the target branch without checking it out"`
	Detach bool `xor:"detach-or-dry-run" help:"Detach HEAD after checking out"`
}

type branchCheckoutCmd struct {
	checkoutOptions

	Untracked bool   `short:"u" config:"branchCheckout.showUntracked" help:"Show untracked branches if one isn't supplied"`
	Branch    string `arg:"" optional:"" help:"Name of the branch to delete" predictor:"branches"`
}

func (*branchCheckoutCmd) Help() string {
	return text.Dedent(`
		A prompt will allow selecting between tracked branches.
		Provide a branch name as an argument to skip the prompt.
		Use -u/--untracked to show untracked branches in the prompt.
	`)
}

func (cmd *branchCheckoutCmd) Run(
	ctx context.Context,
	log *log.Logger,
	view ui.View,
	repo *git.Repository,
	store *state.Store,
	svc *spice.Service,
) error {
	if cmd.Branch == "" {
		if !ui.Interactive(view) {
			return fmt.Errorf("cannot proceed without a branch name: %w", errNoPrompt)
		}

		// If a branch name is not provided,
		// list branches besides the current branch and pick one.
		currentBranch, err := repo.CurrentBranch(ctx)
		if err != nil {
			currentBranch = ""
		}

		cmd.Branch, err = (&branchPrompt{
			Disabled: func(b git.LocalBranch) bool {
				return b.Name != currentBranch && b.CheckedOut
			},
			Default:     currentBranch,
			TrackedOnly: !cmd.Untracked,
			Title:       "Select a branch to checkout",
		}).Run(ctx, view, repo, store)
		if err != nil {
			return fmt.Errorf("select branch: %w", err)
		}
	}

	if cmd.Branch != store.Trunk() {
		if err := svc.VerifyRestacked(ctx, cmd.Branch); err != nil {
			var restackErr *spice.BranchNeedsRestackError
			switch {
			case errors.As(err, &restackErr):
				log.Warnf("%v: needs to be restacked: run 'gs branch restack --branch=%v'", cmd.Branch, cmd.Branch)
			case errors.Is(err, state.ErrNotExist):
				if !ui.Interactive(view) {
					log.Warnf("%v: branch not tracked: run 'gs branch track'", cmd.Branch)
					break
				}

				log.Warnf("%v: branch not tracked", cmd.Branch)
				track := true
				prompt := ui.NewConfirm().
					WithValue(&track).
					WithTitle("Do you want to track this branch now?")
				if err := ui.Run(view, prompt); err != nil {
					return fmt.Errorf("prompt: %w", err)
				}

				if track {
					err := (&branchTrackCmd{
						Branch: cmd.Branch,
					}).Run(ctx, log, repo, store, svc)
					if err != nil {
						return fmt.Errorf("track branch: %w", err)
					}
				}
			case errors.Is(err, git.ErrNotExist):
				return fmt.Errorf("branch %q does not exist", cmd.Branch)
			default:
				log.Warnf("error checking branch: %v", err)
			}
		}
	}

	if cmd.DryRun {
		fmt.Println(cmd.Branch)
		return nil
	}

	if cmd.Detach {
		if err := repo.DetachHead(ctx, cmd.Branch); err != nil {
			return fmt.Errorf("detach HEAD: %w", err)
		}

		return nil
	}

	if err := repo.Checkout(ctx, cmd.Branch); err != nil {
		return fmt.Errorf("checkout branch: %w", err)
	}

	return nil
}
